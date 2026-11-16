package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"maintenance-reminder/internal/auth"
	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/db"
)

// newTestServer builds a server backed by a temporary database with one
// administrator account.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	server, _ := newTestServerWithDB(t)
	return server, "admin"
}

// newTestServerWithDB builds the same server and also hands back its
// database, so that a test can put a record in place before it makes a
// request.
func newTestServerWithDB(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	hash, err := auth.HashPassword("SuperSecret12345")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err = database.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active) VALUES('admin',?,'admin',0,1)`, hash); err != nil {
		t.Fatalf("insert admin: %v", err)
	}

	cfg := config.Config{Timezone: "Asia/Shanghai"}
	cfg.HTTP.SessionHours = 24
	server := httptest.NewServer(New(database, "", cfg).Handler())
	t.Cleanup(server.Close)
	return server, database
}

// login authenticates and returns the session cookie plus the CSRF token.
func login(t *testing.T, server *httptest.Server, username, password string) (*http.Cookie, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	res, err := http.Post(server.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("login status %d", res.StatusCode)
	}
	var out struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	cookie := &http.Cookie{Name: "maintenance_session", Value: res.Cookies()[0].Value}
	return cookie, out.Data.CSRF
}

// do performs an authenticated request and returns the status code and the
// decoded envelope.
func do(t *testing.T, server *httptest.Server, cookie *http.Cookie, csrf, method, path string, payload any) (int, map[string]any) {
	t.Helper()
	var body *bytes.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, server.URL+path, body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s %s: %v", method, path, err)
	}
	return res.StatusCode, out
}

func TestPushTokenLifecycle(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	// Create.
	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "html", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("create failed: %v", out)
	}
	data := out["data"].(map[string]any)
	id := int64(data["id"].(float64))
	if data["token_masked"] == "secret-token" {
		t.Error("expected the raw token to be masked in the response")
	}
	if _, has := data["token"]; has {
		t.Error("expected the raw token to be absent from the response")
	}

	// List.
	_, out = do(t, server, cookie, csrf, http.MethodGet, "/api/push-tokens", nil)
	list := out["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 token, got %d", len(list))
	}

	// Update, keeping the secret by leaving it blank.
	_, out = do(t, server, cookie, csrf, http.MethodPut, "/api/push-tokens/"+itoa(id), map[string]any{
		"name": "运维组 2", "kind": "user", "token": "", "channel": "mail", "template": "txt", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("update failed: %v", out)
	}

	// Disable.
	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(id)+"/enabled", map[string]any{"enabled": false})
	if out["ok"] != true {
		t.Fatalf("disable failed: %v", out)
	}

	// Delete.
	_, out = do(t, server, cookie, csrf, http.MethodDelete, "/api/push-tokens/"+itoa(id), nil)
	if out["ok"] != true {
		t.Fatalf("delete failed: %v", out)
	}

	_, out = do(t, server, cookie, csrf, http.MethodGet, "/api/push-tokens", nil)
	if len(out["data"].([]any)) != 0 {
		t.Error("expected the token to be gone after the delete")
	}
}

func TestPushTokenValidation(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	// Unknown channel.
	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "x", "kind": "user", "token": "t", "channel": "telegram", "enabled": true,
	})
	if out["ok"] == true {
		t.Error("expected an unknown channel to be rejected")
	}

	// Group token without a topic code.
	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "x", "kind": "group", "token": "t", "enabled": true,
	})
	if out["ok"] == true {
		t.Error("expected a group token without a topic code to be rejected")
	}

	// Missing name.
	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "", "kind": "user", "token": "t", "enabled": true,
	})
	if out["ok"] == true {
		t.Error("expected a missing name to be rejected")
	}
}

func TestPushOptions(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	_, out := do(t, server, cookie, csrf, http.MethodGet, "/api/push-options", nil)
	if out["ok"] != true {
		t.Fatalf("options failed: %v", out)
	}
	data := out["data"].(map[string]any)
	templates := data["templates"].([]any)
	if len(templates) == 0 {
		t.Error("expected a non-empty template list")
	}
	channels := data["channels"].([]any)
	foundPaid := false
	for _, c := range channels {
		m := c.(map[string]any)
		if m["value"] == "sms" && m["free"] == false {
			foundPaid = true
		}
	}
	if !foundPaid {
		t.Error("expected the sms channel to be flagged as paid")
	}
}

func TestSettingsExposePushDefaults(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	_, out := do(t, server, cookie, csrf, http.MethodPut, "/api/settings", map[string]any{
		"push.channel": "mail", "push.template": "txt", "push.timeout_seconds": 20,
	})
	if out["ok"] != true {
		t.Fatalf("save settings failed: %v", out)
	}
	_, out = do(t, server, cookie, csrf, http.MethodGet, "/api/settings", nil)
	data := out["data"].(map[string]any)
	if data["push.channel"] != "mail" {
		t.Errorf("expected the channel to persist, got %v", data["push.channel"])
	}
	if data["push.template"] != "txt" {
		t.Errorf("expected the template to persist, got %v", data["push.template"])
	}
	if data["push.timeout_seconds"] != "20" {
		t.Errorf("expected the timeout to persist, got %v", data["push.timeout_seconds"])
	}
}

func TestStrategyCarriesTokenID(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret", "enabled": true,
	})
	id := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "策略 A", "type": "milestone", "enabled": true, "offsets": []int{7}, "template": "内容", "token_id": id,
	})
	if out["ok"] != true {
		t.Fatalf("create strategy failed: %v", out)
	}
	_, out = do(t, server, cookie, csrf, http.MethodGet, "/api/strategies", nil)
	items := out["data"].([]any)
	found := false
	for _, s := range items {
		m := s.(map[string]any)
		if m["name"] == "策略 A" && int64(m["token_id"].(float64)) == id {
			found = true
		}
	}
	if !found {
		t.Error("expected the strategy to keep the chosen token id")
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
