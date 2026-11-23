package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"maintenance-reminder/internal/push"
)

// TestPreviewDeviceRendersRealData checks that the preview of a device shows
// the data of that device, so that the test push shows the real message.
func TestPreviewDeviceRendersRealData(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "市医院项目", "customer": "市第一医院", "contact": "张医生",
	})
	if out["ok"] != true {
		t.Fatalf("create project: %v", out)
	}
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-001",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	if out["ok"] != true {
		t.Fatalf("create device: %v", out)
	}
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+itoa(deviceID), map[string]any{
		"content": "项目：{{project}} 客户：{{customer}} 设备：{{model}} SN：{{sn}} 到期：{{end}} 负责人：{{owner}}",
	})
	if status != 200 {
		t.Fatalf("preview status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	content := data["content"].(string)
	for _, want := range []string{"市医院项目", "市第一医院", "S5720", "SN-001", "2026-12-31"} {
		if !strings.Contains(content, want) {
			t.Errorf("preview content %q missing %q", content, want)
		}
	}
	// The markers must be gone, not left in the text.
	if strings.Contains(content, "{{") {
		t.Errorf("preview content still has a marker: %q", content)
	}
}

// TestPreviewStrategyRejectsUnknownMarker checks that a marker nothing can
// fill is reported, instead of going out half empty. A marker of another
// kind is fine: a strategy may send any template.
func TestPreviewStrategyRejectsUnknownMarker(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
		"kind": "milestone", "content": "到期：{{end}} 数量：{{nope}}",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
	// A marker of another kind is accepted and rendered.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
		"kind": "milestone", "mode": "sample", "content": "到期：{{end}} 数量：{{count}}",
	})
	if status != 200 {
		t.Fatalf("a cross kind marker was refused: %d %v", status, out)
	}
}

// TestPreviewStrategySample checks that the sample mode renders without any
// record in the database.
func TestPreviewStrategySample(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
		"kind": "milestone", "mode": "sample", "content": "项目：{{project}} 剩余：{{days}} 天",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	content := out["data"].(map[string]any)["content"].(string)
	if strings.Contains(content, "{{") {
		t.Errorf("sample content still has a marker: %q", content)
	}
}

// TestTestPushSendsConfirmedContent checks that a test push sends the text
// the user confirmed, and not a built-in message.
func TestTestPushSendsConfirmedContent(t *testing.T) {
	captured := &capturedRequest{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.set(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"执行成功","data":"msg-id-1"}`))
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
	if out["ok"] != true {
		t.Fatalf("create token: %v", out)
	}
	tokenID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(tokenID)+"/test", map[string]any{
		"title": "自定义标题", "content": "这是我改过的内容",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	body, _ := captured.get()
	if body["title"] != "自定义标题" {
		t.Errorf("title = %v, want 自定义标题", body["title"])
	}
	if body["content"] != "这是我改过的内容" {
		t.Errorf("content = %v, want 我改过的内容", body["content"])
	}
}

// TestDraftStrategyTestPush checks that a strategy that has not been saved
// can still be tested.
func TestDraftStrategyTestPush(t *testing.T) {
	captured := &capturedRequest{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.set(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"执行成功","data":"msg-id-2"}`))
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
	if out["ok"] != true {
		t.Fatalf("create token: %v", out)
	}
	tokenID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies/test", map[string]any{
		"title": "草稿标题", "content": "草稿内容", "name": "草稿策略", "token_id": tokenID, "recipients": "[]",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	body, _ := captured.get()
	if body["content"] != "草稿内容" {
		t.Errorf("content = %v, want 草稿内容", body["content"])
	}
}

// TestTestPushIsLogged checks that a test push leaves a trace in the log, so
// that the log page shows what was sent.
func TestTestPushIsLogged(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"执行成功","data":"msg-id-3"}`))
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
	if out["ok"] != true {
		t.Fatalf("create token: %v", out)
	}
	tokenID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(tokenID)+"/test", map[string]any{
		"title": "标题", "content": "内容",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}

	status, out = do(t, server, cookie, csrf, http.MethodGet, "/api/logs?search=token_test_push", nil)
	if status != 200 {
		t.Fatalf("logs status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["total"].(float64) < 1 {
		t.Fatalf("no log entry for the test push: %v", out)
	}
}

// TestGetDeviceReturnsProject checks that one device can be read on its
// own, which the test dialog needs.
func TestGetDeviceReturnsProject(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "监控项目", "customer": "某公司",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "浪潮", "model": "NF5280", "serial_number": "SN-9",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-10-01", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/devices/"+itoa(deviceID), nil)
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["project"] != "监控项目" {
		t.Errorf("project = %v, want 监控项目", data["project"])
	}
	if data["customer"] != "某公司" {
		t.Errorf("customer = %v, want 某公司", data["customer"])
	}
}

// itoa turns an id into the string a path needs.
