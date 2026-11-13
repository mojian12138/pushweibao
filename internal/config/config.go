package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	HTTP struct {
		Listen       string `yaml:"listen"`
		SessionHours int    `yaml:"session_hours"`
		SecureCookie bool   `yaml:"secure_cookie"`
	} `yaml:"http"`
	Database             string   `yaml:"database"`
	Timezone             string   `yaml:"timezone"`
	PushPlusToken        string   `yaml:"pushplus_token"`
	ReminderScanInterval Duration `yaml:"reminder_scan_interval"`
}

func Defaults() Config {
	var c Config
	c.HTTP.SessionHours = 12
	c.Database = "maintenance.db"
	c.Timezone = "Asia/Shanghai"
	c.ReminderScanInterval = Duration(time.Hour)
	return c
}

// Load reads an existing YAML file or returns defaults without writing it.
func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return resolve(c, path)
	}
	if err != nil {
		return c, err
	}
	if err = yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return resolve(c, path)
}

func resolve(c Config, path string) (Config, error) {
	if c.HTTP.SessionHours <= 0 {
		c.HTTP.SessionHours = 12
	}
	if c.Database == "" {
		c.Database = "maintenance.db"
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if c.ReminderScanInterval <= 0 {
		c.ReminderScanInterval = Duration(time.Hour)
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return c, fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}
	if !filepath.IsAbs(c.Database) {
		c.Database = filepath.Join(filepath.Dir(path), c.Database)
	}
	return c, nil
}

func Save(path string, c Config) error {
	persisted := c
	if filepath.IsAbs(persisted.Database) {
		relative, err := filepath.Rel(filepath.Dir(path), persisted.Database)
		if err != nil {
			return err
		}
		persisted.Database = relative
	}
	b, err := yaml.Marshal(persisted)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
