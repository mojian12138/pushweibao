package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// TestPushTemplatesListCarriesTheBuiltins checks that a fresh install
// serves the three built-in templates, so the page has detailed templates
// to show before the user writes one.
func TestPushTemplatesListCarriesTheBuiltins(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/push-templates", nil)
	if status != 200 {
		t.Fatalf("status %d: %v", status, out)
	}
	list, _ := out["data"].([]any)
	if len(list) != 3 {
		t.Fatalf("expected 3 builtin templates, got %d", len(list))
	}
	for _, item := range list {
		x := item.(map[string]any)
		if x["builtin"] != true {
			t.Errorf("the template %v is not builtin", x["name"])
		}
	}
}

// TestPushTemplateCRUD checks that a template can be created, edited and
// deleted, and that the built-in ones are kept.
func TestPushTemplateCRUD(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "我的模板", "kind": "milestone", "format": "markdown",
		"title_template": "【测试】", "body_template": "## 提醒\n\n{{milestone_items}}",
	})
	if status != 200 {
		t.Fatalf("create status %d: %v", status, out)
	}
	id := int64(out["data"].(map[string]any)["id"].(float64))

	status, out = do(t, server, cookie, csrf, http.MethodPut, "/api/push-templates/"+itoa(id), map[string]any{
		"name": "我的模板二", "kind": "milestone", "format": "markdown",
		"title_template": "【测试二】", "body_template": "## 提醒二\n\n{{milestone_items}}",
	})
	if status != 200 {
		t.Fatalf("update status %d: %v", status, out)
	}

	// A body with a marker the kind does not know is refused.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "坏模板", "kind": "cycle", "format": "markdown",
		"body_template": "{{upcoming_section}}",
	})
	if status != 400 {
		t.Fatalf("invalid body accepted: %d %v", status, out)
	}

	// A built-in template survives a delete.
	status, out = do(t, server, cookie, csrf, http.MethodGet, "/api/push-templates", nil)
	list, _ := out["data"].([]any)
	builtinID := ""
	for _, item := range list {
		x := item.(map[string]any)
		if x["builtin"] == true {
			builtinID = itoa(int64(x["id"].(float64)))
			break
		}
	}
	status, _ = do(t, server, cookie, csrf, http.MethodDelete, "/api/push-templates/"+builtinID, nil)
	if status != 400 {
		t.Fatalf("a builtin template was deleted: %d", status)
	}

	status, _ = do(t, server, cookie, csrf, http.MethodDelete, "/api/push-templates/"+itoa(id), nil)
	if status != 200 {
		t.Fatalf("delete status %d", status)
	}
}

// TestStrategyCronTriggerChecks checks that a strategy with a broken cron
// expression is refused and one with a working one is accepted.
func TestStrategyCronTriggerChecks(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "坏计划", "type": "monthly", "enabled": true, "cron_expr": "61 * * * *",
	})
	if status != 400 {
		t.Fatalf("a broken cron expression was accepted: %d %v", status, out)
	}
	if msg, _ := out["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "触发计划无效") {
		t.Errorf("the error does not name the schedule: %v", out)
	}

	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "每日汇总", "type": "monthly", "enabled": true, "horizon_days": 90, "cron_expr": "0 9 * * *",
	})
	if status != 200 {
		t.Fatalf("a working cron expression was refused: %d %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["cron_expr"] != "0 9 * * *" {
		t.Errorf("the schedule was not kept: %v", data["cron_expr"])
	}
}
