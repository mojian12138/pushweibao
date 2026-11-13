package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/push"
)

type Scheduler struct {
	DB     *sql.DB
	Config config.Config
	Push   *push.Client
}

type device struct {
	ID                                                                      int64
	Project, Customer, Contact, Vendor, Model, SN, Start, End, Notes, Owner string
}
type strategy struct {
	ID                                        int64
	Name, Type, Offsets, Template, Recipients string
	Day, Horizon, Threshold, Interval         int
}

func (s *Scheduler) Run(ctx context.Context) {
	interval := time.Duration(s.Config.ReminderScanInterval)
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Scan(ctx); err != nil {
			log.Printf("reminder scan: %v", err)
		}
		if err := s.Deliver(ctx); err != nil {
			log.Printf("notification delivery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) Scan(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='pending' WHERE status='sending'`); err != nil {
		return err
	}
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		return err
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	devices, err := s.devices(ctx)
	if err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,type,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,recipients FROM strategies WHERE enabled=1 ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	strategies := []strategy{}
	for rows.Next() {
		var x strategy
		if err = rows.Scan(&x.ID, &x.Name, &x.Type, &x.Offsets, &x.Day, &x.Horizon, &x.Threshold, &x.Interval, &x.Template, &x.Recipients); err != nil {
			rows.Close()
			return err
		}
		strategies = append(strategies, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range strategies {
		recipients, err := s.recipients(ctx, x.Recipients)
		if err != nil {
			return err
		}
		switch x.Type {
		case "milestone":
			var offsets []int
			if err = json.Unmarshal([]byte(x.Offsets), &offsets); err != nil {
				return err
			}
			for _, d := range devices {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				for _, offset := range offsets {
					if days == offset {
						for _, to := range recipients {
							key := fmt.Sprintf("m:%d:%d:%s:%d:%s", x.ID, d.ID, d.End, offset, to)
							if e = s.enqueue(ctx, key, d, x.Name, fmt.Sprintf("%d 天", offset), to, render(x.Template, d, days)); e != nil {
								return e
							}
						}
					}
				}
			}
		case "cycle":
			if x.Interval < 1 {
				continue
			}
			for _, d := range devices {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				if days > x.Threshold {
					continue
				}
				bucket := (x.Threshold - days) / x.Interval
				for _, to := range recipients {
					key := fmt.Sprintf("c:%d:%d:%s:%d:%s", x.ID, d.ID, d.End, bucket, to)
					if e = s.enqueue(ctx, key, d, x.Name, fmt.Sprintf("周期第 %d 次", bucket+1), to, render(x.Template, d, days)); e != nil {
						return e
					}
				}
			}
		case "monthly":
			if now.Day() != x.Day {
				continue
			}
			monthKey := now.Format("2006-01")
			for _, to := range recipients {
				var upcoming, expired strings.Builder
				upcomingCount, expiredCount := 0, 0
				for _, d := range devices {
					days, e := remaining(d.End, today)
					if e != nil {
						return e
					}
					line := fmt.Sprintf("项目：%s 客户：%s 型号：%s SN：%s 到期：%s 剩余：%d 负责人：%s\n", d.Project, d.Customer, d.Model, d.SN, d.End, days, d.Owner)
					if days < 0 {
						expired.WriteString(line)
						expiredCount++
					} else if days <= x.Horizon {
						upcoming.WriteString(line)
						upcomingCount++
					}
				}
				msg := strings.ReplaceAll(x.Template, "{{month}}", monthKey)
				msg = strings.ReplaceAll(msg, "{{count}}", fmt.Sprint(upcomingCount))
				msg = strings.ReplaceAll(msg, "{{items}}", upcoming.String())
				msg = strings.ReplaceAll(msg, "{{expired}}", expired.String())
				msg = strings.ReplaceAll(msg, "{{expired_count}}", fmt.Sprint(expiredCount))
				key := fmt.Sprintf("month:%d:%s:%s", x.ID, monthKey, to)
				if err = s.enqueue(ctx, key, device{}, x.Name, "月度汇总", to, msg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Scheduler) devices(ctx context.Context) ([]device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,p.name,p.customer,p.contact,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.notes,d.owner FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []device{}
	for rows.Next() {
		var d device
		if err = rows.Scan(&d.ID, &d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &d.Start, &d.End, &d.Notes, &d.Owner); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Scheduler) recipients(ctx context.Context, raw string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT username FROM users WHERE active=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if err = rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Scheduler) enqueue(ctx context.Context, key string, d device, strategy, trigger, to, msg string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO notification_outbox(dedupe_key,device_id,project_name,strategy_name,trigger_point,recipient,message) VALUES(?,?,?,?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING`, key, nullID(d.ID), d.Project, strategy, trigger, to, msg)
	return err
}
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
func remaining(end, today string) (int, error) {
	a, err := time.Parse("2006-01-02", end)
	if err != nil {
		return 0, err
	}
	b, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0, err
	}
	return int(a.Sub(b).Hours() / 24), nil
}
func render(t string, d device, days int) string {
	v := map[string]string{"project": d.Project, "customer": d.Customer, "contact": d.Contact, "vendor": d.Vendor, "model": d.Model, "sn": d.SN, "start": d.Start, "end": d.End, "days": fmt.Sprint(days), "notes": d.Notes, "owner": d.Owner}
	for k, x := range v {
		t = strings.ReplaceAll(t, "{{"+k+"}}", x)
	}
	return t
}

func (s *Scheduler) Deliver(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,recipient,message FROM notification_outbox WHERE status IN ('pending','failed') AND next_attempt<=CURRENT_TIMESTAMP ORDER BY id LIMIT 50`)
	if err != nil {
		return err
	}
	type item struct {
		ID          int64
		To, Message string
	}
	items := []item{}
	for rows.Next() {
		var x item
		if err = rows.Scan(&x.ID, &x.To, &x.Message); err != nil {
			rows.Close()
			return err
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range items {
		result, e := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sending',attempts=attempts+1 WHERE id=? AND status IN ('pending','failed')`, x.ID)
		if e != nil {
			return e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			continue
		}
		token := s.Config.PushPlusToken
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(pushplus_token,'') FROM users WHERE username=? AND active=1`, x.To).Scan(&token)
		sendErr := s.Push.Send(ctx, token, "维保到期提醒", x.Message)
		success := sendErr == nil
		detail := ""
		if sendErr != nil {
			detail = sendErr.Error()
		}
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO notification_attempts(outbox_id,succeeded,detail) VALUES(?,?,?)`, x.ID, boolInt(success), detail)
		if success {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sent',sent_at=CURRENT_TIMESTAMP,last_error='' WHERE id=?`, x.ID)
		} else {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='failed',last_error=?,next_attempt=datetime('now', '+' || MIN(attempts*5,360) || ' minutes') WHERE id=?`, detail, x.ID)
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
