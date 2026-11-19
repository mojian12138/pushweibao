package httpapi

import (
	"net/http"
	"testing"
)

func TestGlobalPushTokenSettingsAreMaskedAndUsed(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	status, out := do(t, server, cookie, csrf, http.MethodPut, "/api/settings", map[string]any{"pushplus_token": "global-secret-token"})
	if status != http.StatusOK || out["ok"] != true {
		t.Fatalf("save global token status=%d response=%v", status, out)
	}
	status, out = do(t, server, cookie, csrf, http.MethodGet, "/api/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("read settings status=%d", status)
	}
	data := out["data"].(map[string]any)
	if data["pushplus_token_configured"] != true || data["pushplus_token"] != "" {
		t.Fatalf("token response must be masked: %v", data)
	}
	var stored string
	if err := database.QueryRow(`SELECT value FROM settings WHERE key='pushplus_token'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "global-secret-token" {
		t.Fatalf("stored global token=%q", stored)
	}
}

func TestStrategyReorderRejectsInvalidListsAndPersistsOrder(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	var ids []int64
	rows, err := database.Query(`SELECT id FROM strategies ORDER BY sort_order,id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) < 2 {
		t.Fatal("expected seeded strategies")
	}
	status, _ := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies/reorder", map[string]any{"ids": []int64{ids[1], ids[0], ids[2]}})
	if status != http.StatusOK {
		t.Fatalf("reorder status=%d", status)
	}
	var first int64
	if err = database.QueryRow(`SELECT id FROM strategies ORDER BY sort_order,id LIMIT 1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if first != ids[1] {
		t.Fatalf("first strategy=%d, want %d", first, ids[1])
	}
	status, _ = do(t, server, cookie, csrf, http.MethodPost, "/api/strategies/reorder", map[string]any{"ids": []int64{ids[1], ids[1]}})
	if status != http.StatusBadRequest {
		t.Fatalf("duplicate ids status=%d, want 400", status)
	}
}
