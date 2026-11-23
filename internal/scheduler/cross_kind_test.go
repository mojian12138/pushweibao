package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"maintenance-reminder/internal/template"
)

// The templates are shared: a strategy of any kind may point at a template
// written for another kind. The tests below guard the promise that comes with
// it — whatever the template asks for, the message that reaches the phone has
// been filled, never a raw marker.

// TestSummaryTemplateOnAMilestoneTriggerIsFilled reproduces the report of a
// strategy that carries a milestone trigger but sends the monthly summary
// template: the message must hold the summary of the window instead of the
// raw markers.
func TestSummaryTemplateOnAMilestoneTriggerIsFilled(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 5, 45)
	id := seedStrategy(t, s, "[90,60,30,7,0]", summaryBody())
	// The title travels with the template, so the strategy carries the one
	// of the monthly summary; a daily schedule makes the window fire.
	if _, err := s.DB.Exec(`UPDATE strategies SET cron_expr='9 15 * * *',title_template='【月度维保汇总】{{month}}' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	scanAt(t, s, 15, 9)
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the summary on a milestone trigger queued %d messages, want 1", count)
	}
	var title, msg string
	if err := s.DB.QueryRow(`SELECT title,message FROM notification_outbox LIMIT 1`).Scan(&title, &msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, title, msg)
	// The month and the count of the summary are filled, and the devices of
	// the window are listed under the blocks of the summary.
	if !strings.Contains(msg, "2026-") {
		t.Errorf("the month is missing from the message: %q", msg)
	}
	if !strings.Contains(msg, "SN-A") || !strings.Contains(msg, "SN-B") {
		t.Errorf("the devices of the window are missing: %q", msg)
	}
	if !strings.Contains(title, "2026-") {
		t.Errorf("the title does not carry the month: %q", title)
	}
}

// TestMilestoneTriggerKeepsItsOwnLayout checks that the window layout of a
// milestone template did not change: the tiers still come from the offsets
// and the overdue devices stay out of a plain reminder.
func TestMilestoneTriggerKeepsItsOwnLayout(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 5, 45, -3)
	seedStrategy(t, s, "[90,60,30,7,0]", "## 📌 维保到期提醒\n\n{{milestone_items}}")
	fixedTime(t, s, 15, 9)
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var msg string
	if err := s.DB.QueryRow(`SELECT message FROM notification_outbox LIMIT 1`).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, msg)
	if strings.Contains(msg, "SN-C") {
		t.Errorf("an overdue device went into a reminder of a coming date: %q", msg)
	}
	if !strings.Contains(msg, "### ⏰ 剩余 ≤7 天 · 1 台") {
		t.Errorf("the tiers of the window are missing: %q", msg)
	}
}

// TestSummaryTemplateFiresOnBothBlocks checks that the overdue devices join
// the summary of a milestone trigger, because a summary asks about them.
func TestSummaryTemplateFiresOnBothBlocks(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 5, -3)
	id := seedStrategy(t, s, "[90,60,30,7,0]", summaryBody())
	if _, err := s.DB.Exec(`UPDATE strategies SET cron_expr='9 15 * * *' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	scanAt(t, s, 15, 9)
	var msg string
	if err := s.DB.QueryRow(`SELECT message FROM notification_outbox LIMIT 1`).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, msg)
	if !strings.Contains(msg, "SN-B") {
		t.Errorf("the overdue device is missing from the summary: %q", msg)
	}
}

// TestCycleTriggerWithASummaryTemplateIsFilled checks the other single-device
// path: a cycle reminder that points at a summary template still comes out
// filled, because it is rendered with the summary of the one device it names.
func TestCycleTriggerWithASummaryTemplateIsFilled(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 20)
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.DB.QueryRow(`SELECT id FROM strategies WHERE type='cycle' ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,cycle_threshold=30,cycle_interval=7,template=?,format=? WHERE id=?`, summaryBody(), template.FormatMarkdown, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var title, msg string
	if err := s.DB.QueryRow(`SELECT title,message FROM notification_outbox LIMIT 1`).Scan(&title, &msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, title, msg)
	if !strings.Contains(msg, "SN-A") {
		t.Errorf("the device is missing from the message: %q", msg)
	}
}

// TestPerDeviceMilestoneWithASummaryTemplateIsFilled covers the plain
// per-device reminder, which is the path a milestone strategy takes when its
// template does not ask for a list.
func TestPerDeviceMilestoneWithASummaryTemplateIsFilled(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 30)
	// The body asks for a summary, so the window path is taken; a body that
	// only names the device markers keeps the per-device path, which is
	// checked here against a summary-shaped title.
	id := seedStrategy(t, s, "[90,60,30,7,0]", "设备 {{project}} / {{sn}} 剩余 {{days}} 天")
	if _, err := s.DB.Exec(`UPDATE strategies SET title_template='【月度维保汇总】{{month}} · {{count}} 台' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var title, msg string
	if err := s.DB.QueryRow(`SELECT title,message FROM notification_outbox LIMIT 1`).Scan(&title, &msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, title, msg)
	if !strings.Contains(title, "2026-") {
		t.Errorf("the month of the title is empty: %q", title)
	}
	if !strings.Contains(title, "1 台") {
		t.Errorf("the count of the title is missing: %q", title)
	}
}

// summaryBody is the body of the built-in monthly template.
func summaryBody() string {
	return "## 📅 {{month}} 维保到期汇总\n\n本月共有 **{{count}}** 台设备即将到期。\n\n{{upcoming_section}}\n\n{{expired_section}}\n\n> 请及时核对设备状态，安排续保。"
}

// assertNoRawMarkers fails when a marker reached the message as raw text.
func assertNoRawMarkers(t *testing.T, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(text, "{{") {
			t.Errorf("a raw marker reached the message: %q", text)
		}
	}
}
// fixedTime points the scheduler at a known minute and day, so that a cron
// schedule fires in a test.
func fixedTime(t *testing.T, s *Scheduler, hour, minute int) {
	t.Helper()
	loc := time.FixedZone("CST", 8*60*60)
	day := time.Date(2026, 11, 23, hour, minute, 0, 0, loc)
	s.Now = func() time.Time { return day }
}

// scanAt runs one scan at the minute given.
func scanAt(t *testing.T, s *Scheduler, hour, minute int) {
	t.Helper()
	fixedTime(t, s, hour, minute)
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestSummaryTitleCountsWhatIsAhead checks that the title and the body of a
// summary count the same devices: the ones that are still ahead, so a reader
// does not see "共 3 台" over a list of 2.
func TestSummaryTitleCountsWhatIsAhead(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 5, 45, -3)
	id := seedStrategy(t, s, "[90,60,30,7,0]", summaryBody())
	if _, err := s.DB.Exec(`UPDATE strategies SET cron_expr='9 15 * * *',title_template='【汇总】{{month}} · 共 {{count}} 台' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	scanAt(t, s, 15, 9)
	var title, msg string
	if err := s.DB.QueryRow(`SELECT title,message FROM notification_outbox LIMIT 1`).Scan(&title, &msg); err != nil {
		t.Fatal(err)
	}
	assertNoRawMarkers(t, title, msg)
	if !strings.Contains(title, "共 2 台") {
		t.Errorf("the title counts every device of the window: %q", title)
	}
	if !strings.Contains(msg, "**2** 台设备即将到期") {
		t.Errorf("the body does not count the devices that are ahead: %q", msg)
	}
}
