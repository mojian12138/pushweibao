package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Listen != "" || c.Database != filepath.Join(filepath.Dir(path), "maintenance.db") {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if time.Duration(c.ReminderScanInterval) != time.Hour {
		t.Fatalf("unexpected interval: %s", time.Duration(c.ReminderScanInterval))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Load should not write config, stat err=%v", err)
	}
}

func TestLoadKeepsEmptyListenForInteractiveSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Listen != "" {
		t.Fatalf("listen = %q, want empty so the first run can prompt", c.HTTP.Listen)
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c := Defaults()
	c.HTTP.Listen = "0.0.0.0:23456"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HTTP.Listen != c.HTTP.Listen {
		t.Fatalf("listen = %q", got.HTTP.Listen)
	}
	if got.Database != filepath.Join(filepath.Dir(path), "maintenance.db") {
		t.Fatalf("database path = %q", got.Database)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("config permissions too broad: %o", info.Mode().Perm())
	}
}

func TestLoadRejectsInvalidTimezone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("timezone: not/a-zone\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}
