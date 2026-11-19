package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"maintenance-reminder/internal/template"
)

// seedStrategy enables the default milestone strategy with the given
// offsets and body, and returns its id, so that the tests of the window
// messages can drive the same strategy the users run.
func seedStrategy(t *testing.T, s *Scheduler, offsets string, body string) int64 {
	t.Helper()
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.DB.QueryRow(`SELECT id FROM strategies WHERE type='milestone' ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,offsets=?,template=?,title_template='',item_template='',format=? WHERE id=?`, offsets, body, template.FormatMarkdown, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// seedDevicesForMilestones writes a project with the devices given, each
// named by its serial number and ending the number of days from today.
func seedDevicesForMilestones(t *testing.T, s *Scheduler, days ...int) {
	t.Helper()
	if _, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active) VALUES('admin','x','admin',0,1)`); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO projects(name,customer,contact,owner) VALUES('项目A','客户','','')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := res.LastInsertId()
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
	for i, d := range days {
		sn := "SN-" + string(rune('A'+i))
		if _, err := s.DB.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status) VALUES(?,?,?,?,?,?,?)`, projectID, "厂商", "型号", sn, today, dateOffset(today, d), "active"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMilestoneWindowGroupsTheDevices checks that the window summary of the
// grouped template covers every device inside the widest offset, grouped
// into the tiers of the strategy, the most urgent tier first.
func TestMilestoneWindowGroupsTheDevices(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	// The devices sit in the 7 day tier, the 30 day tier and the 90 day
	// tier, and one outside the window to prove the window cuts at 90.
	seedDevicesForMilestones(t, s, 5, 12, 45, 120)
	seedStrategy(t, s, "[90,60,30,7,0]", "## 📌 维保到期提醒\n\n{{milestone_items}}")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one window message, got %d", count)
	}
	var msg, trigger string
	if err := s.DB.QueryRow(`SELECT message,trigger_point FROM notification_outbox LIMIT 1`).Scan(&msg, &trigger); err != nil {
		t.Fatal(err)
	}
	// The device that expires in 120 days stays outside the window.
	if strings.Contains(msg, "SN-D") {
		t.Errorf("a device outside the window was included: %q", msg)
	}
	// The tiers carry a heading of their own and come in urgency order.
	// The device 45 days out falls into the 60 day tier, so no ≤90 tier
	// shows up in this message.
	tier0 := strings.Index(msg, "### ⏰ 剩余 ≤7 天 · 1 台")
	tier30 := strings.Index(msg, "### ⏰ 剩余 ≤30 天 · 1 台")
	tier60 := strings.Index(msg, "### ⏰ 剩余 ≤60 天 · 1 台")
	if tier0 < 0 || tier30 < 0 || tier60 < 0 {
		t.Fatalf("a tier heading is missing: %q", msg)
	}
	if !(tier0 < tier30 && tier30 < tier60) {
		t.Errorf("the tiers are not in urgency order: %q", msg)
	}
	for _, sn := range []string{"SN-A", "SN-B", "SN-C"} {
		if !strings.Contains(msg, sn) {
			t.Errorf("the device %s is missing from the message: %q", sn, msg)
		}
	}
	if !strings.Contains(trigger, "0 天") || !strings.Contains(trigger, "90 天") {
		t.Errorf("the trigger point does not name the tiers: %q", trigger)
	}
}

// TestMilestoneWindowSortsInsideTheTier checks that two devices of the same
// tier are ordered by the days that are left, so the most urgent one is
// read first.
func TestMilestoneWindowSortsInsideTheTier(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 45, 12)
	seedStrategy(t, s, "[90,60,30,7,0]", "## 📌 维保到期提醒\n\n{{milestone_items}}")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var msg string
	if err := s.DB.QueryRow(`SELECT message FROM notification_outbox LIMIT 1`).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	if strings.Index(msg, "SN-B") > strings.Index(msg, "SN-A") {
		t.Errorf("the devices are not sorted by urgency: %q", msg)
	}
}

// TestMilestoneWindowDoesNotRepeatOnTheSameDay checks that a second scan on
// the same day does not queue the window message again.
func TestMilestoneWindowDoesNotRepeatOnTheSameDay(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 12)
	seedStrategy(t, s, "[30]", "## 📌 维保到期提醒\n\n{{milestone_items}}")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the window message was queued %d times", count)
	}
}

// TestMilestoneWindowSkipsAnEmptyWindow checks that a window without
// devices sends nothing, instead of a message without a list.
func TestMilestoneWindowSkipsAnEmptyWindow(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 400)
	seedStrategy(t, s, "[30]", "## 📌 维保到期提醒\n\n{{milestone_items}}")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("an empty window was queued %d times", count)
	}
}

// TestMilestoneScanKeepsOneMessagePerDeviceForOldTemplates checks that a
// template the user wrote without the grouped marker still sends one
// message per device on the day the offset is reached.
func TestMilestoneScanKeepsOneMessagePerDeviceForOldTemplates(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 30, 30)
	seedStrategy(t, s, "[30]", "【维保提醒】{{project}} 剩余 {{days}} 天")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected one message per device, got %d", count)
	}
}

// TestMonthlyScanOrdersTheDevicesByUrgency checks that the summary shows
// the devices that run out first at the top and carries a block of its own
// for the devices that already expired.
func TestMonthlyScanOrdersTheDevicesByUrgency(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 45, 10, 0, -5)
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,monthly_day=1,horizon_days=90,format=?,template=?,title_template='',item_template='' WHERE type='monthly'`, template.FormatMarkdown,
		"## 📅 {{month}} 维保到期汇总\n\n{{upcoming_section}}\n\n{{expired_section}}"); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var msg string
	if err := s.DB.QueryRow(`SELECT message FROM notification_outbox WHERE dedupe_key LIKE 'month:%'`).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "### 🔔 即将到期 · 3 台") {
		t.Errorf("the upcoming block is missing: %q", msg)
	}
	if !strings.Contains(msg, "### ⛔ 已过期 · 1 台") {
		t.Errorf("the expired block is missing: %q", msg)
	}
	// The device with 0 days left sits above the one with 10 days, and the
	// device that expired 5 days ago opens the expired block.
	d0 := strings.Index(msg, "SN-C")
	d10 := strings.Index(msg, "SN-B")
	if d0 < 0 || d10 < 0 {
		t.Fatalf("the summary lost a device: %q", msg)
	}
	if d10 < d0 {
		t.Errorf("the devices are not in urgency order: %q", msg)
	}
}

// TestMonthlyScanSkipsAnEmptySummary checks that a month without devices
// does not send an empty summary.
func TestMonthlyScanSkipsAnEmptySummary(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	seedDevicesForMilestones(t, s, 400)
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,monthly_day=1,horizon_days=90 WHERE type='monthly'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("an empty summary was queued %d times", count)
	}
}
