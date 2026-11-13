package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/db"
)

func TestPromptPasswordRequiresLengthAndMatch(t *testing.T) {
	answers := []string{"short", "longenoughpassword", "differentpassword12", "longenoughpassword", "longenoughpassword"}
	index := 0
	read := func() (string, error) {
		if index >= len(answers) {
			return "", os.ErrClosed
		}
		value := answers[index]
		index++
		return value, nil
	}
	var out strings.Builder
	password, err := promptPasswordWith(read, &out)
	if err != nil {
		t.Fatal(err)
	}
	if password != "longenoughpassword" {
		t.Fatalf("password = %q", password)
	}
	text := out.String()
	if !strings.Contains(text, "至少需要 12 位") || !strings.Contains(text, "两次密码不一致") {
		t.Fatalf("missing validation feedback: %s", text)
	}
}

func TestPromptPasswordFailsAfterThreeAttempts(t *testing.T) {
	read := func() (string, error) { return "short", nil }
	var out strings.Builder
	if _, err := promptPasswordWith(read, &out); err == nil {
		t.Fatal("expected failure")
	}
}

func TestFirstRunWritesConfigAndSingleAdmin(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(dir, "maintenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var out strings.Builder
	listener, err := firstRunWith(configPath, &cfg, database, bufio.NewReader(strings.NewReader("\n")), &out, func() (string, string, error) {
		return "admin", "longenoughpassword", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.HTTP.Listen != listener.Addr().String() {
		t.Fatalf("saved listen %q, want %q", saved.HTTP.Listen, listener.Addr())
	}
	if !strings.Contains(saved.HTTP.Listen, "0.0.0.0:") {
		t.Fatalf("unexpected listen address %q", saved.HTTP.Listen)
	}
	var admins int
	if err = database.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Fatalf("admin count = %d", admins)
	}
	var hash string
	if err = database.QueryRow(`SELECT password_hash FROM users WHERE role='admin'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "longenoughpassword") {
		t.Fatal("plaintext password was stored")
	}
}

func TestFirstRunWithoutCredentialsKeepsExistingAdmin(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(dir, "maintenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.Exec(`INSERT INTO users(username,password_hash,role,must_change_password) VALUES('admin','x','admin',0)`); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	listener, err := firstRunWith(configPath, &cfg, database, bufio.NewReader(strings.NewReader("\n")), &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var admins int
	if err = database.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Fatalf("admin count = %d, want 1", admins)
	}
}
