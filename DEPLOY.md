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

下文的部署有两种方式，任选其一：

- **宝塔面板** —— 用「进程守护管理器」托管，全程图形界面，推荐给不熟悉命令行的场景（见第六节）；
- **纯命令行** —— 用 systemd 托管（见第二节）。

## 二、部署步骤（systemd 方式）

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

### 3. 用 systemd 托管（若用宝塔面板，见第六节，可跳过本节）

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

## 六、用宝塔面板常驻运行

宝塔上不需要手写 systemd，用「进程守护管理器」插件托管即可，崩溃会自动拉起、开机自动启动。

### 1. 上传程序

宝塔面板 →「文件」→ 进入 `/www/wwwroot/`，新建目录 `weibao`，把
`maintenance-reminder-linux-amd64` 上传进去，然后**重命名为 `maintenance-reminder`**。

上传后右键该文件 →「权限」→ 权限设为 `755`，并勾选「可执行」（或在终端执行
`chmod +x /www/wwwroot/weibao/maintenance-reminder`）。这一步漏了会报「权限不足」。

### 2. 首次初始化（在宝塔终端里手动跑一次）

宝塔面板右上角「终端」（或 SSH 登录）执行：

```bash
cd /www/wwwroot/weibao
./maintenance-reminder
```

按提示依次输入：端口（直接回车用推荐的）→ 管理员用户名（回车用 `admin`）→ 密码（至少 12 位，输两次）。
看到「初始化完成，监听 0.0.0.0:端口」后按 `Ctrl+C` 退出。

> 首次初始化必须有交互终端，直接在守护进程里启动会因为读不到键盘输入而失败。

### 3. 安装进程守护插件

宝塔面板 →「软件商店」→ 搜索「进程守护管理器」（部分版本叫「Supervisor 管理器」）→ 安装。

### 4. 添加守护进程

打开插件 →「添加守护进程」，按下表填写：

| 字段 | 填写内容 |
| :--- | :--- |
| 名称 | `weibao`（随意，英文） |
| 运行目录 | `/www/wwwroot/weibao` |
| 启动命令 | `/www/wwwroot/weibao/maintenance-reminder -config /www/wwwroot/weibao/config.yaml` |
| 运行用户 | `root`（图省事）；用 `www` 更安全，但要先 `chown -R www:www /www/wwwroot/weibao` |
| 进程数量 | `1` |
| 描述 | 维保提醒系统 |

保存后状态应显示「运行中」。插件里可以看实时日志，也可以手动「重启 / 停止」。

> **启动命令必须写绝对路径**。插件里的「运行目录」只决定进程的工作目录，
> supervisord 查找命令时并不进入那个目录，写 `./maintenance-reminder` 会一直
> 报 `spawnerr: can't find command './maintenance-reminder'`，然后进程进入
> FATAL 状态、反复重试。改成 `/www/wwwroot/weibao/maintenance-reminder` 即可。
>
> 如果报 `permission denied`，说明文件没有执行权限：
> `chmod +x /www/wwwroot/weibao/maintenance-reminder`。

### 5. 开放端口

两步都要做，缺一个浏览器就打不开：

1. **宝塔面板** →「安全」→ 添加端口规则，放行你初始化时用的那个端口；
2. **云厂商控制台** → 安全组 → 入方向放行同一端口（阿里云/腾讯云必做）。

### 6. 访问

浏览器打开 `http://服务器IP:端口`，用第 2 步设置的账号登录。
登录后到「系统设置 → 令牌管理」填 PushPlus 令牌，再到「通知策略」里点「测试推送」确认能收到微信。

### 7. 可选：用域名 + HTTPS 访问

如果希望用域名访问并启用 HTTPS：

1. 宝塔「网站」→ 添加站点（纯静态即可，PHP 版本选「纯静态」）；
2. 站点设置 →「反向代理」→ 添加，目标 URL 填 `http://127.0.0.1:端口`，发送域名填 `$host`；
3. 站点设置 →「SSL」→ 申请 Let's Encrypt 证书并开启「强制 HTTPS」；
4. 把 `config.yaml` 改成 `listen: "127.0.0.1:端口"` 和 `secure_cookie: true`，然后在守护插件里重启。

改成 `127.0.0.1` 后程序只监听本机，外部只能经 Nginx 进来，更安全。

### 8. 升级

宝塔「文件」里把新的 `maintenance-reminder` 覆盖上去（保持 755），
然后在守护插件里点「重启」。**升级前先在网页里「数据备份与恢复」导出一份。**

## 七、常见问题

**启动报 `bind ... address already in use`**
端口被占用。改 `config.yaml` 里的端口，或用 `ss -lntp | grep 端口号` 找占用进程。

**非交互式终端报错“首次运行需要交互式终端”**
说明还没有管理员账号（或 `config.yaml` 里 `http.listen` 为空）。先按第二节第 2 步手工跑一次。

**网页能打开但推送收不到**
检查「系统设置」里的令牌、「通知策略」里的接收人，以及 `journalctl` 里的推送日志。

**忘记管理员密码**
停掉服务，用备份的数据库覆盖，或重新初始化一个新库再导入备份文件。