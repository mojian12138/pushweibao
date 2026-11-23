package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"maintenance-reminder/internal/template"
)

// The templates are shared, so a strategy of one kind may point at a template
// written for another. The preview is the screen where the user sees the
// message before saving, so it has to fill every marker: a preview that shows
// "{{month}}" is the first sign that the phone will show it too.

// summaryTemplate is the body of the built-in monthly template.
const summaryTemplate = "## 📅 {{month}} 维保到期汇总\n\n本月共有 **{{count}}** 台设备即将到期。\n\n{{upcoming_section}}\n\n{{expired_section}}\n\n> 请及时核对设备状态，安排续保。"

// assertFilled fails when a raw marker survived the render.
func assertFilled(t *testing.T, where, title, content string) {
	t.Helper()
	if strings.Contains(title, "{{") {
		t.Errorf("%s: the title kept a raw marker: %q", where, title)
	}
	if strings.Contains(content, "{{") {
		t.Errorf("%s: the body kept a raw marker: %q", where, content)
	}
}

// TestPreviewFillsASummaryTemplateOnAMilestoneStrategy is the reported bug:
// a milestone strategy pointing at the monthly template showed the raw
// markers in the strategy list and in the editor.
func TestPreviewFillsASummaryTemplateOnAMilestoneStrategy(t *testing.T) {
	server, _ := newTestServer(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")

	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
		"kind": template.KindMilestone, "mode": "sample",
		"title": "【月度维保汇总】{{month}}", "content": summaryTemplate, "format": "markdown",
	})
	if status != http.StatusOK {
		t.Fatalf("preview status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	assertFilled(t, "sample preview", data["title"].(string), data["content"].(string))
	if !strings.Contains(data["content"].(string), "即将到期") {
		t.Errorf("the summary blocks are missing from the preview: %q", data["content"])
	}

	// The real mode reads the database, so a device is put in place first.
	status, out = do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
		"kind": template.KindMilestone, "mode": "real",
		"title": "【月度维保汇总】{{month}}", "content": summaryTemplate, "format": "markdown",
	})
	if status != http.StatusOK {
		t.Fatalf("real preview status %d: %v", status, out)
	}
	data = out["data"].(map[string]any)
	assertFilled(t, "real preview", data["title"].(string), data["content"].(string))
}

// TestStrategyListShowsAFilledSummary checks the card of the strategy list,
// which renders the same template with the sample data.
func TestStrategyListShowsAFilledSummary(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	// A milestone strategy that sends the monthly template, exactly the
	// shape a user creates when they pick the summary from the list.
	body := summaryTemplate
	if _, err := database.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE strategies SET enabled=1,type='milestone',template=?,title_template='【月度维保汇总】{{month}}',format=? WHERE type='milestone'`, body, template.FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	status, out := do(t, server, cookie, csrf, http.MethodGet, "/api/strategies", nil)
	if status != http.StatusOK {
		t.Fatalf("strategies status %d", status)
	}
	rows := out["data"].([]any)
	if len(rows) == 0 {
		t.Fatal("no strategy came back")
	}
	found := false
	for _, item := range rows {
		row := item.(map[string]any)
		preview, _ := row["preview"].(string)
		if !strings.Contains(preview, "即将到期") {
			continue
		}
		found = true
		if strings.Contains(preview, "{{") {
			t.Errorf("the card of the strategy kept a raw marker: %q", preview)
		}
	}
	if !found {
		t.Errorf("no card showed the summary blocks: %v", rows)
	}
}

// TestPreviewFillsEveryKindOfTemplate checks that a template of each kind
// comes out filled whichever strategy kind it is previewed under, because a
// strategy may point at any of them.
func TestPreviewFillsEveryKindOfTemplate(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	// The real mode reads a record, so one is put in place.
	res, err := database.Exec(`INSERT INTO projects(name,customer,contact,owner) VALUES('项目A','客户','联系人','负责人')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := res.LastInsertId()
	if _, err = database.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status) VALUES(?,?,?,?,?,?,?)`,
		projectID, "厂商", "型号", "SN-1", "2026-01-01", "2026-12-01", "active"); err != nil {
		t.Fatal(err)
	}
	templates := map[string]string{
		"milestone": "## 📌 提醒\n\n{{milestone_items}}\n\n剩余 {{days}} 天",
		"monthly":   summaryTemplate,
		"cycle":     "设备 {{project}} / {{vendor}} {{model}} / SN {{sn}} 剩余 {{days}} 天",
	}
	for _, kind := range []string{template.KindMilestone, template.KindMonthly, template.KindCycle} {
		for name, body := range templates {
			for _, mode := range []string{"sample", "real"} {
				status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/strategy", map[string]any{
					"kind": kind, "mode": mode, "title": "【提醒】{{month}}{{count}}", "content": body, "format": "markdown",
				})
				if status != http.StatusOK {
					t.Fatalf("%s/%s/%s: status %d: %v", kind, name, mode, status, out)
				}
				data := out["data"].(map[string]any)
				assertFilled(t, kind+" strategy previewing a "+name+" template in "+mode+" mode", data["title"].(string), data["content"].(string))
			}
		}
	}
}

// TestDeviceTestPreviewFillsASummaryTemplate covers the test push of a device
// that points at a summary template.
func TestDeviceTestPreviewFillsASummaryTemplate(t *testing.T) {
	server, database := newTestServerWithDB(t)
	cookie, csrf := login(t, server, "admin", "SuperSecret12345")
	res, err := database.Exec(`INSERT INTO projects(name,customer,contact,owner) VALUES('项目A','客户','联系人','负责人')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := res.LastInsertId()
	res, err = database.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status) VALUES(?,?,?,?,?,?,?)`,
		projectID, "厂商", "型号", "SN-1", "2026-01-01", "2026-12-01", "active")
	if err != nil {
		t.Fatal(err)
	}
	deviceID, _ := res.LastInsertId()
	status, out := do(t, server, cookie, csrf, http.MethodPost, "/api/preview/device/"+strconv.FormatInt(deviceID, 10), map[string]any{
		"title": "【汇总】{{month}}", "content": summaryTemplate, "format": "markdown",
	})
	if status != http.StatusOK {
		t.Fatalf("device preview status %d: %v", status, out)
	}
	data := out["data"].(map[string]any)
	assertFilled(t, "device preview", data["title"].(string), data["content"].(string))
	if !strings.Contains(data["content"].(string), "SN-1") {
		t.Errorf("the device is missing from the preview: %q", data["content"])
	}
}
