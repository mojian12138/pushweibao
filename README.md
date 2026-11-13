# 项目设备维保到期提醒系统

单进程 Go 服务，使用纯 Go `modernc.org/sqlite` SQLite 驱动。静态 HTML、CSS、JavaScript 和 SQL 迁移嵌入可执行文件，不需要 Node、CGO、Docker、MySQL、Redis 或 Nginx。

## 首次运行

将 `maintenance-reminder` 可执行文件、`config.yaml` 和 `maintenance.db` 放在同一目录。首次运行时数据库会自动创建并迁移，默认策略会写入数据库。首次配置会提示选择端口并要求设置管理员用户名与密码（至少 12 位，输入不回显）。

默认配置模板中的 `http.listen` 为空，首次启动会进入交互式初始化：程序扫描 `20000–50000` 高位端口并占用一个可用端口作为推荐值；可直接回车接受，或输入其他 `1024–65535` 端口，程序会实际绑定验证。随后设置管理员用户名和两次输入的密码（至少 12 位，密码输入不回显，最多 3 次机会）。配置写入 `config.yaml` 后，后续重启不再询问。若数据库中已存在管理员账号，则只询问端口，不再要求重新设置凭据。

服务监听 `0.0.0.0:<端口>`，可用 `http://服务器公网IP:<端口>` 或解析到该服务器的域名访问。端口必须在云安全组和主机防火墙中放行；如通过云服务器反向代理，代理上游使用 `127.0.0.1:<端口>`（同机）或服务器内网 IP 与该端口（跨机），此时只需放行代理的 80/443 端口。域名由 DNS 指向代理或服务器，不需要填入应用监听配置。生产环境建议由反向代理提供 HTTPS 并设置 `secure_cookie: true`；仅更换高位端口不是安全措施。

## 构建

需要 Go 1.24 或更新版本。前端没有构建步骤。

```sh
go mod tidy
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o maintenance-reminder ./cmd/maintenance-reminder
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o maintenance-reminder ./cmd/maintenance-reminder
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o maintenance-reminder ./cmd/maintenance-reminder
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o maintenance-reminder ./cmd/maintenance-reminder
```

## 启动

```sh
./maintenance-reminder -config ./config.yaml
```

Linux/macOS 首次启动均会创建或更新配置指定的数据库。移动部署时停止服务后，将可执行文件、`config.yaml` 和 `.db` 文件一起复制；配置中的数据库使用相对路径时以配置文件目录为基准。SQLite 使用 DELETE journal，保持单数据库文件交付。备份接口使用 SQLite `VACUUM INTO` 生成一致快照。

## 账号与通知

角色为 `admin` 和 `viewer`。管理员可进行所有写操作、账号管理和系统设置；viewer 只能查看看板、项目/设备和通知记录。默认策略包含里程碑 `[90,60,30,7,0]`、每月 1 日汇总未来 90 天和停用的周期提醒。通知事件先写入 outbox 并用唯一触发键去重，再由后台 worker 发送；失败记录可手动重试。PushPlus 支持系统全局 Token 与账号独立 Token。
