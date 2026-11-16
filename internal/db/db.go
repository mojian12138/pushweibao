package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(DELETE)")
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := migrate(database); err != nil {
		database.Close()
		return nil, err
	}
	if err := backfillPushTokens(database); err != nil {
		database.Close()
		return nil, err
	}
	// A strategy that still carries the plain text of the first release is
	// moved to the styled preset, so that a program started on an existing
	// database sends styled messages without an edit of every strategy.
	if err := upgradeLegacyStrategies(database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

// backfillPushTokens copies legacy PushPlus tokens into the push_tokens table
// and binds them to the accounts that used them. It runs on every start and is
// idempotent, so tokens configured by an older build are picked up as well.
func backfillPushTokens(database *sql.DB) error {
	statements := []string{
		// The legacy global token becomes a normal token, unless a token with
		// the same value already exists.
		`INSERT INTO push_tokens(name,kind,token_value,channel,template,enabled,note)
		 SELECT '默认令牌','user',value,'wechat','html',1,'由旧版全局 PushPlus Token 自动迁移'
		 FROM settings WHERE key='pushplus_token' AND value<>''
		 AND NOT EXISTS (SELECT 1 FROM push_tokens WHERE token_value=(SELECT value FROM settings WHERE key='pushplus_token'))`,
		`INSERT INTO push_tokens(name,kind,token_value,channel,template,enabled,note)
		 SELECT u.username||' 的令牌','user',u.pushplus_token,'wechat','html',1,'由旧版账号 Token 自动迁移'
		 FROM users u WHERE u.pushplus_token<>''
		 AND NOT EXISTS (SELECT 1 FROM push_tokens WHERE token_value=u.pushplus_token)`,
		`UPDATE users SET push_token_id=(SELECT t.id FROM push_tokens t WHERE t.token_value=users.pushplus_token AND t.kind='user')
		 WHERE pushplus_token<>'' AND push_token_id IS NULL`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("backfill push tokens: %w", err)
		}
	}
	return nil
}

func migrate(database *sql.DB) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		var exists int
		if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name=?`, filepath.Base(name)).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		content, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := database.Begin()
		if err != nil {
			return err
		}
		statements := strings.Split(string(content), ";")
		for _, statement := range statements {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = tx.Exec(statement); err != nil {
				break
			}
		}
		if err == nil {
			_, err = tx.Exec(`INSERT INTO schema_migrations(name) VALUES(?)`, filepath.Base(name))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
