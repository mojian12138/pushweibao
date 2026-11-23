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

	// A body with a marker nothing knows is refused; a marker of another
	// kind is fine, because a strategy may send any template.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "坏模板", "kind": "cycle", "format": "markdown",
		"body_template": "{{nope}}",
	})
	if status != 400 {
		t.Fatalf("invalid body accepted: %d %v", status, out)
	}
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "跨类型模板", "kind": "cycle", "format": "markdown",
		"body_template": "{{upcoming_section}}",
	})
	if status != 200 {
		t.Fatalf("a cross kind marker was refused: %d %v", status, out)
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

// TestStrategyListWithBoundTemplateDoesNotDeadlock checks that a strategy
// pointing at a template is listed without the server waiting on itself:
// the row loop of the list used to ask the database for the template while
// the outer rows still held the only connection, and every request of the
// server froze until the list timed out.
func TestStrategyListWithBoundTemplateDoesNotDeadlock(t *testing.T) {
	server, _ := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "回归模板", "kind": "milestone", "format": "markdown",
		"title_template": "【回归】", "body_template": "## 提醒\n\n{{milestone_items}}",
	})
	if status != 200 {
		t.Fatalf("create template: %d %v", status, out)
	}
	tplID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "绑定模板的策略", "type": "milestone", "enabled": true, "template_id": tplID,
	})
	if status != 200 {
		t.Fatalf("create strategy: %d %v", status, out)
	}

	// The list has to come back while the database keeps its single
	// connection: a deadlock here would hang the request until the client
	// gave up.
	status, out = do(t, server, cookie, csrf, http.MethodGet, "/api/strategies", nil)
	if status != 200 {
		t.Fatalf("list strategies: %d %v", status, out)
	}
	list, _ := out["data"].([]any)
	var found map[string]any
	for _, item := range list {
		x := item.(map[string]any)
		if x["template_name"] == "回归模板" {
			found = x
		}
	}
	if found == nil {
		t.Fatalf("the bound template was not shown: %v", list)
	}
	if found["template"] != "## 提醒\n\n{{milestone_items}}" {
		t.Errorf("the words of the template were not used: %v", found["template"])
	}
}

// TestStrategyKeepsItsTriggerWithAForeignTemplate checks that a strategy
// may send a template written for another kind and still keep its own
// trigger: the template supplies the words, the trigger decides when the
// message goes out.
func TestStrategyKeepsItsTriggerWithAForeignTemplate(t *testing.T) {
	server, _ := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	// A summary template, pointed at by a milestone strategy.
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/push-templates", map[string]any{
		"name": "汇总样式", "kind": "monthly", "format": "markdown",
		"title_template": "【汇总】{{month}}",
		"body_template":  "## 汇总\n\n{{upcoming_section}}\n\n{{expired_section}}",
		"item_template":  "| {{project}} | {{sn}} | {{days}} 天 |",
	})
	if status != 200 {
		t.Fatalf("create template: %d %v", status, out)
	}
	tplID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/strategies", map[string]any{
		"name": "里程碑用汇总模板", "type": "milestone", "enabled": true,
		"template_id": tplID, "cron_expr": "0 9 * * *", "offsets": []int{90, 60, 30, 7, 0},
	})
	if status != 200 {
		t.Fatalf("create strategy: %d %v", status, out)
	}
	created := out["data"].(map[string]any)
	if created["type"] != "milestone" {
		t.Errorf("the template overwrote the trigger type: %v", created["type"])
	}
	if created["cron_expr"] != "0 9 * * *" {
		t.Errorf("the trigger schedule was lost: %v", created["cron_expr"])
	}

	// The list shows the same trigger type and the words of the template.
	status, out = do(t, server, cookie, csrf, http.MethodGet, "/api/strategies", nil)
	if status != 200 {
		t.Fatalf("list: %d %v", status, out)
	}
	var found map[string]any
	for _, item := range out["data"].([]any) {
		x := item.(map[string]any)
		if x["name"] == "里程碑用汇总模板" {
			found = x
		}
	}
	if found == nil {
		t.Fatal("the strategy is missing from the list")
	}
	if found["type"] != "milestone" {
		t.Errorf("the list changed the trigger type to %v", found["type"])
	}
	if found["template_name"] != "汇总样式" {
		t.Errorf("the template is not shown: %v", found["template_name"])
	}
}
