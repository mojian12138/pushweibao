package db

import (
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndSeedsStrategies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var strategies int
	if err = database.QueryRow(`SELECT COUNT(*) FROM strategies`).Scan(&strategies); err != nil {
		t.Fatal(err)
	}
	if strategies != 3 {
		t.Fatalf("expected 3 default strategies, got %d", strategies)
	}
	if err = migrate(database); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT COUNT(*) FROM strategies`).Scan(&strategies); err != nil {
		t.Fatal(err)
	}
	if strategies != 3 {
		t.Fatalf("migration rerun changed seed count: %d", strategies)
	}
	var fk int
	if err = database.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys disabled: %d, %v", fk, err)
	}
}
