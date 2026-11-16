package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"maintenance-reminder/internal/push"
)

// TestDeviceTestPushSendsRenderedContent walks through the exact steps the
// device test dialog takes: ask for a preview, then send what came back.
//
// The bug this guards against: the dialog sent the template with its markers
// still in it, so the message that arrived read "项目：{{project}}".
func TestDeviceTestPushSendsRenderedContent(t *testing.T) {
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

	// A push needs a token to go out with, so one is created first.
	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "txt", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("create token: %v", out)
	}

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "市医院项目", "customer": "市第一医院", "contact": "张医生",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-001",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	// The text the dialog starts with, the same one the page ships.
	const raw = "项目：{{project}}\n客户：{{customer}}\n设备：{{model}} SN：{{sn}}\n到期：{{end}}（剩余 {{days}} 天）\n负责人：{{owner}}"

	// Step one: the preview.
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+itoa(deviceID), map[string]any{
		"mode": "real", "title": "", "content": raw,
	})
	if status != 200 {
		t.Fatalf("preview status %d: %v", status, out)
	}
	preview := out["data"].(map[string]any)
	content := preview["content"].(string)

	// The preview must not carry a single marker.
	if strings.Contains(content, "{{") {
		t.Fatalf("the preview still has a marker: %q", content)
	}
	for _, want := range []string{"市医院项目", "市第一医院", "S5720", "SN-001", "2026-12-31"} {
		if !strings.Contains(content, want) {
			t.Errorf("the preview is missing %q: %q", want, content)
		}
	}

	// Step two: send what the preview returned.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices/"+itoa(deviceID)+"/test-push", map[string]any{
		"title": preview["title"], "content": content,
	})
	if status != 200 {
		t.Fatalf("test push status %d: %v", status, out)
	}

	// What arrived must be the rendered text.
	body, _ := captured.get()
	sent, _ := body["content"].(string)
	if strings.Contains(sent, "{{") {
		t.Fatalf("the message that arrived still has a marker: %q", sent)
	}
	if sent != content {
		t.Errorf("the message that arrived differs from the preview:\nsent: %q\nshown: %q", sent, content)
	}
}

// TestDeviceTestPushRejectsMarkers checks that the server refuses to send a
// message that still carries a marker, so that a broken dialog cannot send
// a half rendered message again.
func TestDeviceTestPushRejectsMarkers(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "项目", "customer": "客户",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-1",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/devices/"+itoa(deviceID)+"/test-push", map[string]any{
		"title": "标题", "content": "项目：{{project}} 客户：{{customer}}",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
}

// TestDraftStrategyTestPushRejectsMarkers checks the same for a strategy that
// has not been saved yet.
func TestDraftStrategyTestPushRejectsMarkers(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies/test", map[string]any{
		"title": "标题", "content": "项目：{{project}} 客户：{{customer}}", "name": "草稿",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
}

// TestStrategyTestPushRejectsMarkers checks the same for a saved strategy.
func TestStrategyTestPushRejectsMarkers(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "里程碑", "type": "milestone", "enabled": true, "offsets": []int{30},
		"template": "项目：{{project}} 客户：{{customer}}", "title_template": "",
	})
	if out["ok"] != true {
		t.Fatalf("create strategy: %v", out)
	}
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies/"+itoa(id)+"/test", map[string]any{
		"title": "标题", "content": "项目：{{project}} 客户：{{customer}}",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
}

// TestTokenTestPushRejectsMarkers checks the same for a token.
func TestTokenTestPushRejectsMarkers(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "txt", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("create token: %v", out)
	}
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(id)+"/test", map[string]any{
		"title": "标题", "content": "项目：{{project}} 客户：{{customer}}",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
}

// TestTokenTestPushSendsRenderedContent walks through the exact steps the
// token test dialog takes: ask for a preview, then send what came back.
//
// The bug this guards against: the dialog sent the template with its markers
// still in it, so the message that arrived read "项目：{{project}}".
func TestTokenTestPushSendsRenderedContent(t *testing.T) {
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

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "市医院项目", "customer": "市第一医院",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-001",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	if out["ok"] != true {
		t.Fatalf("create device: %v", out)
	}

	// The text the dialog starts with, the same one the page ships.
	const raw = "项目：{{project}}\n客户：{{customer}}\n设备：{{model}} SN：{{sn}}\n到期：{{end}}（剩余 {{days}} 天）"

	// Step one: the preview.
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/token", map[string]any{
		"mode": "real", "title": "", "content": raw,
	})
	if status != 200 {
		t.Fatalf("preview status %d: %v", status, out)
	}
	preview := out["data"].(map[string]any)
	content := preview["content"].(string)
	if strings.Contains(content, "{{") {
		t.Fatalf("the preview still has a marker: %q", content)
	}

	// Step two: send what the preview returned.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(tokenID)+"/test", map[string]any{
		"title": preview["title"], "content": content,
	})
	if status != 200 {
		t.Fatalf("test push status %d: %v", status, out)
	}

	// What arrived must be the rendered text.
	body, _ := captured.get()
	sent, _ := body["content"].(string)
	if strings.Contains(sent, "{{") {
		t.Fatalf("the message that arrived still has a marker: %q", sent)
	}
	if sent != content {
		t.Errorf("the message that arrived differs from the preview:\nsent: %q\nshown: %q", sent, content)
	}
}

// TestPreviewDeviceRejectsMarkers is the guard on the preview itself: a
// template with a marker the kind does not know must not come back rendered
// with the marker left in it.
func TestPreviewDeviceRejectsMarkers(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "项目", "customer": "客户",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-1",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	// "count" belongs to the monthly summary, not to a single device.
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+itoa(deviceID), map[string]any{
		"mode": "real", "title": "", "content": "数量：{{count}}",
	})
	if status != 400 {
		t.Fatalf("status %d, want 400: %v", status, out)
	}
}

// TestPreviewDeviceKeepsNewlines checks that the line breaks of a multi line
// template survive the round trip, so the message reads the same in the
// preview as on the phone.
func TestPreviewDeviceKeepsNewlines(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "项目", "customer": "客户",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-1",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+itoa(deviceID), map[string]any{
		"mode": "real", "title": "", "content": "第一行\n第二行\n第三行",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	content := out["data"].(map[string]any)["content"].(string)
	if content != "第一行\n第二行\n第三行" {
		t.Errorf("the line breaks changed: %q", content)
	}
}

// TestPreviewDeviceTitleIsRendered checks that the title goes through the
// renderer too.
func TestPreviewDeviceTitleIsRendered(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/projects", map[string]any{
		"name": "市医院项目", "customer": "客户",
	})
	projectID := int64(out["data"].(map[string]any)["id"].(float64))

	_, out = do(t, server, cookie, csrf, http.MethodPost, "/api/devices", map[string]any{
		"project_id": projectID, "vendor": "华为", "model": "S5720", "serial_number": "SN-1",
		"maintenance_start": "2026-01-01", "maintenance_end": "2026-12-31", "status": "active",
	})
	deviceID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+itoa(deviceID), map[string]any{
		"mode": "real", "title": "{{project}} 设备到期提醒", "content": "正文",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	title := out["data"].(map[string]any)["title"].(string)
	if title != "市医院项目 设备到期提醒" {
		t.Errorf("title = %q", title)
	}
}

// The JSON decoder of the server refuses unknown fields, so the frontend and
// the server must agree on the names. This test keeps that agreement.
func TestPreviewRequestFieldNames(t *testing.T) {
	var req previewRequest
	raw := `{"kind":"milestone","mode":"real","title":"t","content":"c","item":"i","device_id":3}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Kind != "milestone" || req.Mode != "real" || req.Title != "t" || req.Content != "c" || req.Item != "i" || req.DeviceID != 3 {
		t.Errorf("the fields do not match: %+v", req)
	}
}
