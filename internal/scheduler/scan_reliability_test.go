package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestScanCatchesUpMilestonesOnceAndHonorsDeviceOverride(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	var firstID int64
	if err := s.DB.QueryRow(`SELECT id FROM strategies WHERE name='默认里程碑提醒'`).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,offsets='[10]',recipients='["admin"]' WHERE id=?`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role,active) VALUES('admin','x','admin',1)`); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO strategies(name,type,enabled,sort_order,offsets,template,recipients) VALUES('覆盖策略','milestone',1,20,'[10]','{{project}}','["admin"]')`)
	if err != nil {
		t.Fatal(err)
	}
	overrideID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	var projectID int64
	res, err = s.DB.Exec(`INSERT INTO projects(name,customer,contact,owner,notes,contract_no) VALUES('项目','客户','','','','')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err = res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
	end := dateOffset(today, 10)
	if _, err = s.DB.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,strategy_override) VALUES(?,?,?,?,?,?,?,?)`, projectID, "厂商", "型号", "SN-OVERRIDE", today, end, "active", overrideID); err != nil {
		t.Fatal(err)
	}
	previous := dateOffset(today, -2)
	if _, err = s.DB.Exec(`INSERT INTO settings(key,value) VALUES('scheduler.last_scan_date',?)`, previous); err != nil {
		t.Fatal(err)
	}
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var names string
	if err = s.DB.QueryRow(`SELECT group_concat(strategy_name, ',') FROM notification_outbox`).Scan(&names); err != nil {
		t.Fatal(err)
	}
	want := "覆盖策略"
	if names != want {
		t.Fatalf("queued strategies=%q, want only %q", names, want)
	}
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same-day rescan queued %d messages, want 1", count)
	}
}

func TestScanQueuesMilestoneWhenFirstRunningOnExactNode(t *testing.T) {
	s := newScheduler(t)
	s.Config.PushPlusToken = "global-test-token"
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE strategies SET enabled=1,offsets='[10]',recipients='["admin"]' WHERE name='默认里程碑提醒'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role,active) VALUES('admin','x','admin',1)`); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO projects(name) VALUES('首次扫描项目')`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
	end := dateOffset(today, 10)
	if _, err = s.DB.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status) VALUES(?,?,?,?,?,?,?)`, projectID, "厂商", "型号", "SN-FIRST", today, end, "active"); err != nil {
		t.Fatal(err)
	}
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("first scan queued %d milestone messages, want 1", count)
	}
}

func dateOffset(today string, days int) string {
	date, err := time.Parse("2006-01-02", today)
	if err != nil {
		panic(err)
	}
	return date.AddDate(0, 0, days).Format("2006-01-02")
}
