# 部署到 Linux 服务器

程序是一个不依赖任何运行时的单一可执行文件：网页、模板和数据库结构都编译在里面，
服务器上不需要安装 Go、Node，也不需要另外上传 `web/` 目录。

## 一、需要上传的文件

| 文件 | 必需 | 说明 |
| :--- | :--- | :--- |
| `maintenance-reminder-linux-amd64` | 是 | 程序本体（Intel/AMD 服务器，绝大多数云主机） |
| `maintenance-reminder-linux-arm64` | 二选一 | 程序本体（ARM 服务器，如部分云厂商的倚天/鲲鹏实例） |
| `config.yaml` | 可选 | 配置文件；首次运行会自动生成，一般不必上传 |

**不要上传**：

- `maintenance.db` —— 服务器首次运行会自动建库。需要带上现有数据时，用下面的“迁移数据”方式。
- `maintenance-reminder`（没有 `-linux-` 后缀的那个）—— 那是 macOS 版本，Linux 上跑不起来。
- 源码目录、`web/`、`dist/`、`.gocache/` —— 都已编译进程序，传了也用不上。

## 二、部署步骤

### 1. 上传

```bash
# 在你自己电脑上执行，把文件放到服务器 /opt/维保提醒 目录
ssh root@你的服务器 'mkdir -p /opt/maintenance-reminder'
scp maintenance-reminder-linux-amd64 root@你的服务器:/opt/maintenance-reminder/maintenance-reminder
```

如果服务器是 ARM 架构，把 `amd64` 换成 `arm64`。不确定时在服务器上执行 `uname -m`：
`x86_64` 用 amd64，`aarch64` 用 arm64。

### 2. 首次运行（必须在 SSH 终端里手动跑一次）

```bash
ssh root@你的服务器
cd /opt/maintenance-reminder
chmod +x maintenance-reminder
./maintenance-reminder
```

首次运行是**交互式**的，会依次询问：

1. **端口** —— 程序会在 20000–50000 之间挑一个未被占用的端口推荐给你，直接回车使用即可（也可自己输入 1024–65535 之间的端口）；
2. **管理员用户名** —— 直接回车即用 `admin`；
3. **管理员密码** —— 至少 12 位，输入时不显示，需要输入两次。

完成后会在当前目录生成 `config.yaml` 和 `maintenance.db`，并打印：

```
初始化完成，监听 0.0.0.0:23456。可使用服务器 IP 或域名访问；请配置云防火墙/安全组。
```

按 `Ctrl+C` 结束，然后交给 systemd 托管。

> 注意：首次运行必须是交互式终端。直接在 systemd 里启动会因为读不到终端而退出。

### 3. 用 systemd 托管

```bash
cat > /etc/systemd/system/maintenance-reminder.service <<'EOF'
[Unit]
Description=维保提醒系统
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/maintenance-reminder
ExecStart=/opt/maintenance-reminder/maintenance-reminder -config /opt/maintenance-reminder/config.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now maintenance-reminder
systemctl status maintenance-reminder
```

查看运行日志（推送、定时任务都在这里）：

```bash
journalctl -u maintenance-reminder -f
```

### 4. 放开端口

云服务器要在**控制台的安全组**里放行你设置的端口；如果服务器开了 `ufw` 或 `firewalld`：

```bash
ufw allow 23456/tcp       # ufw
firewall-cmd --add-port=23456/tcp --permanent && firewall-cmd --reload   # firewalld
```

### 5. 打开网页

浏览器访问 `http://服务器IP:端口`（端口就是初始化时屏幕打印的那个），用刚才设置的管理员账号登录。
建议随后在「系统设置」里绑定推送令牌，再用「通知策略 → 测试推送」确认能收到微信。

## 三、迁移现有数据

有两种方式，任选其一。

### 方式 A：用备份文件（推荐，跨机器最稳）

1. 在**旧机器**上打开「系统设置 → 数据备份与恢复 → 下载备份文件」；
2. 在新服务器上装好并登录后，同一位置点「选择备份文件并恢复」，选覆盖恢复。

备份包含项目、设备、策略、模板、令牌、账号和系统设置；不含日志与推送记录（避免把旧提醒重发一遍）。

### 方式 B：直接拷贝数据库文件

```bash
# 在旧机器上停掉程序，然后上传数据库
scp maintenance.db root@你的服务器:/opt/maintenance-reminder/maintenance.db
```

这种方式日志和历史推送记录也会一并带过去，但要求新服务器**还没初始化过**，
或者先停掉服务再覆盖。覆盖后重启服务即可。

## 四、日常维护

### 升级

```bash
systemctl stop maintenance-reminder
# 上传新的 maintenance-reminder，覆盖旧文件
chmod +x /opt/maintenance-reminder/maintenance-reminder
systemctl start maintenance-reminder
```

数据库结构的升级在程序启动时自动完成，不需要手工执行 SQL。**升级前建议先在网页里备份一次。**

### 备份

- 网页「系统设置 → 数据备份与恢复 → 下载备份文件」，存到另一台电脑或网盘；
- 覆盖恢复前，系统会自动把当前数据另存在 `maintenance.db` 旁边的 `backups/` 目录，保留最近 10 份。

### 文件说明

| 路径 | 内容 |
| :--- | :--- |
| `/opt/maintenance-reminder/maintenance-reminder` | 程序 |
| `/opt/maintenance-reminder/config.yaml` | 配置（端口、时区、数据库位置） |
| `/opt/maintenance-reminder/maintenance.db` | 数据库，**所有数据都在这里** |
| `/opt/maintenance-reminder/backups/` | 恢复前的自动副本 |

这个目录就是全部数据，整个目录拷走等于完整备份。

## 五、配置项

`config.yaml` 可以手工修改，改完重启服务生效：

```yaml
http:
  listen: "0.0.0.0:23456"    # 监听地址与端口；只允许本机访问写 127.0.0.1:23456
  session_hours: 12          # 登录保持时长
  secure_cookie: false       # 用 HTTPS 反代时改成 true
database: maintenance.db     # 相对路径以 config.yaml 所在目录为准
timezone: Asia/Shanghai      # 定时推送按这个时区计算
reminder_scan_interval: 1m    # 扫描间隔；定时推送需要一分钟级别的精度
pushplus_token: ""           # 也可以不写，在网页里配置令牌
```

## 六、常见问题

**启动报 `bind ... address already in use`**
端口被占用。改 `config.yaml` 里的端口，或用 `ss -lntp | grep 端口号` 找占用进程。

**非交互式终端报错“首次运行需要交互式终端”**
说明还没有管理员账号（或 `config.yaml` 里 `http.listen` 为空）。先按第二节第 2 步手工跑一次。

**网页能打开但推送收不到**
检查「系统设置」里的令牌、「通知策略」里的接收人，以及 `journalctl` 里的推送日志。

**忘记管理员密码**
停掉服务，用备份的数据库覆盖，或重新初始化一个新库再导入备份文件。