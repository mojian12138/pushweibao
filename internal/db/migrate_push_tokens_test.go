package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigratePushTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err = first.Exec(`INSERT INTO settings(key,value) VALUES('pushplus_token','be342ac001aa47a9ba57fc7ede25cd77')`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if _, err = first.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,pushplus_token) VALUES('alice','x','admin',0,'be342ac001aa47a9ba57fc7ede25cd77')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	first.Close()

	// Reopen so the new migration runs against the existing data.
	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	var count int
	if err = second.QueryRow(`SELECT COUNT(*) FROM push_tokens`).Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the legacy token to be migrated into push_tokens")
	}

	var tokenID int64
	if err = second.QueryRow(`SELECT push_token_id FROM users WHERE username='alice'`).Scan(&tokenID); err != nil {
		t.Fatalf("user token id: %v", err)
	}
	if tokenID == 0 {
		t.Fatal("expected the account to be bound to a migrated token")
	}

	// The migration must be idempotent.
	third, err := Open(path)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	defer third.Close()
	if err = third.QueryRow(`SELECT COUNT(*) FROM push_tokens`).Scan(&count); err != nil {
		t.Fatalf("recount tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 token after re-running migrations, got %d", count)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("stat db: %v", err)
	}
}
