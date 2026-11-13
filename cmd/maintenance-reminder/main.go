package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"maintenance-reminder/internal/auth"
	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/db"
	"maintenance-reminder/internal/httpapi"
	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/scheduler"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	database, err := db.Open(cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	var count int
	if err = database.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND active=1`).Scan(&count); err != nil {
		database.Close()
		log.Fatalf("check administrator: %v", err)
	}
	if cfg.HTTP.Listen == "" || count == 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			database.Close()
			log.Fatalf("首次运行需要交互式终端，请在服务器控制台运行程序完成端口和管理员初始化")
		}
		listener, setupErr := firstRun(*configPath, &cfg, database, fdLineReader{file: os.Stdin}, os.Stdin, os.Stdout, count == 0)
		if setupErr != nil {
			database.Close()
			log.Fatalf("首次初始化失败：%v", setupErr)
		}
		serve(cfg, database, listener)
		return
	}
	listener, err := net.Listen("tcp4", cfg.HTTP.Listen)
	if err != nil {
		database.Close()
		log.Fatalf("bind %s: %v (port may already be in use)", cfg.HTTP.Listen, err)
	}
	serve(cfg, database, listener)
}

func firstRun(configPath string, cfg *config.Config, database *sql.DB, reader lineReader, in *os.File, out *os.File, createAccount bool) (net.Listener, error) {
	var credentials func() (string, string, error)
	if createAccount {
		credentials = func() (string, string, error) {
			return promptCredentials(reader, in, out)
		}
	}
	return firstRunWith(configPath, cfg, database, reader, out, credentials)
}

// promptCredentials asks for the administrator username and a confirmed hidden
// password on the terminal.
func promptCredentials(reader lineReader, in *os.File, out *os.File) (string, string, error) {
	fmt.Fprintf(out, "管理员用户名（回车使用 admin）：")
	username, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return "", "", err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if err = validateUsername(username); err != nil {
		return "", "", err
	}
	password, err := promptPassword(in, out)
	if err != nil {
		return "", "", err
	}
	return username, password, nil
}

// firstRunWith performs the interactive setup using an injected credential
// provider so the flow can be tested without a terminal. A nil provider skips
// account creation.
func firstRunWith(configPath string, cfg *config.Config, database *sql.DB, reader lineReader, out io.Writer, credentials func() (string, string, error)) (net.Listener, error) {
	var listener net.Listener
	var err error
	if cfg.HTTP.Listen == "" {
		listener, err = chooseListener(reader, out)
	} else {
		listener, err = net.Listen("tcp4", cfg.HTTP.Listen)
	}
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = listener.Close()
		}
	}()
	if credentials != nil {
		username, password, credErr := credentials()
		if credErr != nil {
			return nil, credErr
		}
		if err = validateUsername(username); err != nil {
			return nil, err
		}
		if err = createAdmin(database, username, password); err != nil {
			return nil, err
		}
	} else if err = verifyAdmin(database); err != nil {
		return nil, err
	}
	cfg.HTTP.Listen = listener.Addr().String()
	if err = config.Save(configPath, *cfg); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "初始化完成，监听 %s。可使用服务器 IP 或域名访问；请配置云防火墙/安全组。\n", cfg.HTTP.Listen)
	keep = true
	return listener, nil
}

func serve(cfg config.Config, database *sql.DB, listener net.Listener) {
	defer database.Close()
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	jobs := &scheduler.Scheduler{DB: database, Config: cfg, Push: push.NewClient()}
	go jobs.Run(ctx)
	server := &http.Server{Handler: httpapi.New(database, cfg.Database, cfg).Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("maintenance reminder listening on %s", listener.Addr())
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func validateUsername(username string) error {
	if len(username) > 64 || strings.ContainsAny(username, " \t\r\n") {
		return fmt.Errorf("用户名不能包含空白且不能超过 64 个字符")
	}
	return nil
}

func createAdmin(database interface {
	Exec(string, ...any) (sql.Result, error)
}, username, password string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = database.Exec(`INSERT INTO users(username,password_hash,role,must_change_password) VALUES(?,?,'admin',0)`, username, hash)
	return err
}

func verifyAdmin(database interface{ QueryRow(string, ...any) *sql.Row }) error {
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND active=1`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("数据库中没有启用的管理员账号；请确认数据库文件或重新完成初始化")
	}
	return nil
}
