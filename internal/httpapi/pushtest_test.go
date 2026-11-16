package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"maintenance-reminder/internal/push"
)

// capturedRequest keeps the body that was sent to PushPlus.
type capturedRequest struct {
	mu   sync.Mutex
	body map[string]any
	raw  string
}

func (c *capturedRequest) set(raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = string(raw)
	_ = json.Unmarshal(raw, &c.body)
}

func (c *capturedRequest) get() (map[string]any, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.body, c.raw
}

// TestTestPushSendsNoTimestampField is the regression test for the failure
// that blocked every message: the request carried "timestamp":0, which
// PushPlus read as the epoch and refused as stale.
func TestTestPushSendsNoTimestampField(t *testing.T) {
	captured := &capturedRequest{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.set(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"执行成功","data":"msg-id-1"}`))
	}))
	defer fake.Close()

	// Point the client at the fake server. The endpoint is a package level
	// variable, so it is replaced for the duration of the test.
	original := push.Endpoint
	push.Endpoint = fake.URL
	defer func() { push.Endpoint = original }()

	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "txt", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("create failed: %v", out)
	}
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(id)+"/test", map[string]any{})
	if status != 200 {
		t.Fatalf("test push failed with %d: %v", status, out)
	}

	body, raw := captured.get()
	if _, has := body["timestamp"]; has {
		t.Errorf("the request must not carry a timestamp, got %s", raw)
	}
	if strings.Contains(raw, "timestamp") {
		t.Errorf("the request body mentions a timestamp: %s", raw)
	}
	// The token itself has to be there.
	if body["token"] != "secret-token" {
		t.Errorf("expected the token to be sent, got %v", body["token"])
	}
}

// TestTestPushReportsTheServerReason checks that the reason PushPlus gives is
// shown to the operator instead of a generic message.
func TestTestPushReportsTheServerReason(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":999,"msg":"服务端验证错误","data":"webhook配置信息不存在"}`))
	}))
	defer fake.Close()

	original := push.Endpoint
	push.Endpoint = fake.URL
	defer func() { push.Endpoint = original }()

	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "txt", "enabled": true,
	})
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(id)+"/test", map[string]any{})
	if status != 502 {
		t.Fatalf("expected a 502 for a failed push, got %d", status)
	}
	errObj, _ := out["error"].(map[string]any)
	message, _ := errObj["message"].(string)
	if !strings.Contains(message, "webhook配置信息不存在") {
		t.Errorf("expected the server reason in the message, got %q", message)
	}
}
