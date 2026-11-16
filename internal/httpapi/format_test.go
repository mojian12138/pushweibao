package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/template"
)

// TestMessageFormatsListsTheThreeFormats checks that the editor is told
// about the three formats and the ready-made templates of each one, so that
// it does not have to hardcode them.
func TestMessageFormatsListsTheThreeFormats(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/message-formats", nil)
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	list, _ := out["data"].([]any)
	if len(list) != len(template.Formats) {
		t.Fatalf("expected %d formats, got %d", len(template.Formats), len(list))
	}
	for _, item := range list {
		f := item.(map[string]any)
		presets, _ := f["presets"].(map[string]any)
		if len(presets) == 0 {
			t.Errorf("the format %v has no preset", f["value"])
		}
		if f["hint"] == "" {
			t.Errorf("the format %v has no hint", f["value"])
		}
	}
}

// TestStrategyGetsThePresetOfItsFormat checks that a strategy saved
// without a message gets the ready-made template of its own format, so that
// every format works right away.
func TestStrategyGetsThePresetOfItsFormat(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "Markdown 策略", "type": "milestone", "format": "markdown",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	body, _ := data["template"].(string)
	if body != template.DefaultBodyFor(template.FormatMarkdown, "milestone") {
		t.Errorf("expected the markdown preset, got %q", body)
	}
	title, _ := data["title_template"].(string)
	if title != template.DefaultTitleFor(template.FormatMarkdown, "milestone") {
		t.Errorf("expected the markdown title, got %q", title)
	}
}

// TestTestPushUsesTheChosenFormat checks that a test push is sent with the
// template of the format the user chose, not the one of the token.
func TestTestPushUsesTheChosenFormat(t *testing.T) {
	captured := &capturedRequest{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.set(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":"msg-id-1"}`))
	}))
	defer fake.Close()
	original := push.Endpoint
	push.Endpoint = fake.URL
	defer func() { push.Endpoint = original }()

	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	_, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens", map[string]any{
		"name": "运维组", "kind": "user", "token": "secret-token", "channel": "wechat", "template": "html", "enabled": true,
	})
	if out["ok"] != true {
		t.Fatalf("create failed: %v", out)
	}
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-tokens/"+itoa(id)+"/test", map[string]any{
		"title": "标题", "content": "正文", "format": "markdown",
	})
	if status != 200 {
		t.Fatalf("test push failed with %d: %v", status, out)
	}
	body, raw := captured.get()
	if body["template"] != "markdown" {
		t.Errorf("expected the markdown template, got %v (%s)", body["template"], raw)
	}
	if body["content"] != "正文" {
		t.Errorf("expected the content to be sent as it is, got %v", body["content"])
	}
}

// TestPreviewReportsTheFormat checks that the preview tells the editor
// which format it rendered, so that the preview is shown the way the phone
// will show it.
func TestPreviewReportsTheFormat(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/token", map[string]any{
		"mode": "sample", "content": "项目：{{project}}", "format": "markdown",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["format"] != "markdown" {
		t.Errorf("expected the format to be reported, got %v", data["format"])
	}
	content, _ := data["content"].(string)
	if strings.Contains(content, "{{") {
		t.Errorf("the preview still has a marker: %q", content)
	}
}

// TestPreviewSendsTheMessageAsHTML checks that the preview carries the
// message as html, so that the editor can show the layout of the phone
// instead of the source of the message.
func TestPreviewSendsTheMessageAsHTML(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/token", map[string]any{
		"mode": "sample", "content": template.DefaultBodyFor(template.FormatMarkdown, "milestone"),
		"format": "markdown",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if !strings.Contains(html, "<table>") {
		t.Errorf("the html of the preview has no table: %q", html)
	}
	if !strings.Contains(html, "<h2>") {
		t.Errorf("the html of the preview has no title: %q", html)
	}
}

// TestPreviewOfAnHTMLMessageKeepsTheLayout checks that an html message is
// shown as it is, so that the preview is the layout of the phone.
func TestPreviewOfAnHTMLMessageKeepsTheLayout(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/token", map[string]any{
		"mode": "sample", "content": template.DefaultBodyFor(template.FormatHTML, "milestone"),
		"format": "html",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if !strings.Contains(html, "border-radius") {
		t.Errorf("the html of the preview lost the layout: %q", html)
	}
	if !strings.Contains(html, "<table") {
		t.Errorf("the html of the preview has no table: %q", html)
	}
}

// TestDevicePreviewSendsTheMessageAsHTML checks that the preview of a device
// carries the layout as well, so that the test push of a device is shown the
// way it will arrive.
func TestDevicePreviewSendsTheMessageAsHTML(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	if _, err := database.Exec(`INSERT INTO projects(name,customer) VALUES('市医院','张三')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end) VALUES(1,'H3C','XC-200','SN001','2026-01-01','2026-12-31')`); err != nil {
		t.Fatal(err)
	}
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/1", map[string]any{
		"content": template.DefaultBodyFor(template.FormatHTML, "milestone"), "format": "html",
	})
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if !strings.Contains(html, "border-radius") {
		t.Errorf("the html of the device preview lost the layout: %q", html)
	}
	if strings.Contains(html, "{{") {
		t.Errorf("the device preview still has a marker: %q", html)
	}
}

// TestStrategyListShowsTheRenderedMessage checks that the list carries the
// message as it will look, so that the page is not a wall of template source.
func TestStrategyListShowsTheRenderedMessage(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	// A strategy that is written in html is shown in the list as the card
	// the phone will draw.
	if _, err := database.Exec(`UPDATE strategies SET format='html',template=? WHERE type='milestone'`,
		template.DefaultBodyFor(template.FormatHTML, template.KindMilestone)); err != nil {
		t.Fatal(err)
	}

	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/strategies", nil)
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	list, _ := out["data"].([]any)
	if len(list) == 0 {
		t.Fatal("the list is empty")
	}
	found := false
	for _, item := range list {
		x := item.(map[string]any)
		preview, _ := x["preview"].(string)
		if x["type"] != "milestone" {
			continue
		}
		found = true
		if !strings.Contains(preview, "border-radius") {
			t.Errorf("the html strategy has no layout in the list: %q", preview)
		}
		if strings.Contains(preview, "{{") {
			t.Errorf("the preview of the list still has a marker: %q", preview)
		}
	}
	if !found {
		t.Fatal("no milestone strategy in the list")
	}
}
