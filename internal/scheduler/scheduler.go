package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/config"
	"maintenance-reminder/internal/cron"
	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/systemlog"
	"maintenance-reminder/internal/template"
)

// DefaultTitle is the title used when a strategy has no title template.
const DefaultTitle = "维保到期提醒"

// DefaultItemTemplate is the layout of one line of the monthly summary used
// when a strategy has no item template.
const DefaultItemTemplate = "项目：{{project}} 客户：{{customer}} 型号：{{model}} SN：{{sn}} 到期：{{end}} 剩余：{{days}} 负责人：{{owner}}\n"

// firstNonEmpty returns the first value that is not blank.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type Scheduler struct {
	DB     *sql.DB
	Config config.Config
	Push   *push.Client
	// Log records what the scheduler does, so that the log page can show it.
	Log *systemlog.Logger
}

type device struct {
	ID                                                                      int64
	Project, Customer, Contact, Vendor, Model, SN, Start, End, Notes, Owner string
	StrategyOverride                                                        int64
}
type strategy struct {
	ID                                                                     int64
	Name, Type, Offsets, Template, TitleTemplate, ItemTemplate, Recipients string
	Day, Horizon, Threshold, Interval                                      int
	TokenID                                                                int64
	// Format is the way the message is written: html, txt or markdown. It
	// also decides the PushPlus template the message is sent with.
	Format string
	// CronExpr is the schedule the strategy fires on. An empty expression
	// keeps the trigger of the type: the milestone fires on the day a
	// device reaches a node, the monthly one fires on its monthly day.
	CronExpr string
}

func (s *Scheduler) Run(ctx context.Context) {
	interval := time.Duration(s.Config.ReminderScanInterval)
	if interval <= 0 {
		interval = time.Hour
	}
	// Cron expressions have minute precision. Check at least once a minute
	// and align checks to wall-clock minute boundaries; a ticker started at
	// process boot would drift and could miss a schedule such as 16:58.
	if interval > time.Minute {
		interval = time.Minute
	}
	for {
		if err := s.Scan(ctx); err != nil {
			log.Printf("reminder scan: %v", err)
		}
		if err := s.Deliver(ctx); err != nil {
			log.Printf("notification delivery: %v", err)
		}
		now := time.Now()
		untilBoundary := now.Truncate(time.Minute).Add(time.Minute).Sub(now)
		delay := interval
		if untilBoundary < delay {
			delay = untilBoundary
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Scheduler) Scan(ctx context.Context) error {
	// Recover only expired delivery leases; do not reset requests still in flight.
	if _, err := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='failed',last_error='投递租约过期，等待重试',next_attempt=CURRENT_TIMESTAMP WHERE status='sending' AND next_attempt<=CURRENT_TIMESTAMP`); err != nil {
		return err
	}
	if timezone := s.globalTimezone(ctx); timezone != "" {
		s.Config.Timezone = timezone
	}
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		return err
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	var previousScan string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='scheduler.last_scan_date'`).Scan(&previousScan)
	devices, err := s.devices(ctx)
	if err != nil {
		return err
	}
	applicableDevices := func(strategyID int64) []device {
		out := make([]device, 0, len(devices))
		for _, d := range devices {
			if d.StrategyOverride == 0 || d.StrategyOverride == strategyID {
				out = append(out, d)
			}
		}
		return out
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,type,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,title_template,item_template,recipients,COALESCE(token_id,0),format,COALESCE(cron_expr,'') FROM strategies WHERE enabled=1 ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	strategies := []strategy{}
	for rows.Next() {
		var x strategy
		if err = rows.Scan(&x.ID, &x.Name, &x.Type, &x.Offsets, &x.Day, &x.Horizon, &x.Threshold, &x.Interval, &x.Template, &x.TitleTemplate, &x.ItemTemplate, &x.Recipients, &x.TokenID, &x.Format, &x.CronExpr); err != nil {
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
		targets, err := s.targets(ctx, x.TokenID, recipients)
		if err != nil {
			return err
		}
		switch x.Type {
		case "milestone":
			var offsets []int
			if err = json.Unmarshal([]byte(x.Offsets), &offsets); err != nil {
				return err
			}
			// A template that asks for {{milestone_items}} is a window
			// summary: every scan puts all the devices that still run
			// inside the widest offset into one message, grouped by the
			// tier their remaining days fall into. The message changes
			// every day, so its dedupe key carries the day. The other
			// templates keep the behaviour of the first release: a device
			// that reaches one of the offsets is reminded on that day, and
			// a scan that missed a day catches up without repeating.
			grouped := false
			for _, key := range template.Markers(x.Template) {
				if key == "milestone_items" {
					grouped = true
					break
				}
			}
			if grouped {
				// The window message fires on the cron schedule when the
				// strategy carries one, or on the first scan of the day
				// otherwise. The dedupe key carries the fire time, so a
				// schedule that fires several times a day sends one
				// message per fire, while the same fire on a repeated
				// scan does not repeat the message.
				var fireKey string
				if strings.TrimSpace(x.CronExpr) != "" {
					sched, e := cron.Parse(x.CronExpr)
					if e != nil {
						return fmt.Errorf("策略 %s 的 cron 表达式无效: %w", x.Name, e)
					}
					if !sched.Matches(now) {
						continue
					}
					fireKey = now.Format("2006-01-02T15:04")
				} else {
					fireKey = today
				}
				widest := 0
				for _, o := range offsets {
					if o > widest {
						widest = o
					}
				}
				windowHits := []template.MilestoneHit{}
				for _, d := range applicableDevices(x.ID) {
					days, e := remaining(d.End, today)
					if e != nil {
						return e
					}
					if days < 0 || days > widest {
						continue
					}
					windowHits = append(windowHits, template.MilestoneHit{Device: d.asTemplateDevice(), ID: d.ID, Days: days})
				}
				if len(windowHits) == 0 {
					continue
				}
				items := template.BuildMilestoneItems(x.Format, offsets, windowHits)
				// The representative device carries the markers of a single
				// device: the most urgent one of the window, so that
				// {{days}} and {{end}} name the device that needs
				// attention first.
				rep := windowHits[0]
				for _, h := range windowHits[1:] {
					if h.Days < rep.Days {
						rep = h
					}
				}
				trigger := "窗口汇总"
				if strings.TrimSpace(x.CronExpr) != "" {
					trigger = cron.Describe(x.CronExpr)
				} else {
					triggerLabels := []string{}
					for _, o := range template.MilestoneTiers(offsets) {
						triggerLabels = append(triggerLabels, fmt.Sprintf("%d 天", o))
					}
					trigger = strings.Join(triggerLabels, "、")
				}
				// The strategy may point at a template written for any
				// kind, so the renderer fills the device markers, the
				// grouped list and the summary blocks together. The blocks
				// of a summary are laid out from the same window devices,
				// so a summary template chosen for a window trigger still
				// shows its list.
				rows := make([]template.SummaryRow, 0, len(windowHits))
				for _, h := range windowHits {
					rows = append(rows, template.SummaryRow{Device: h.Device, Days: h.Days})
				}
				upSection, exSection := template.BuildSummaryBlocks(x.Format, x.ItemTemplate, rows)
				upLines, exLines := template.BuildSummaryLines(x.ItemTemplate, rows)
				windowSummary := template.Summary{
					Month:           now.Format("2006-01"),
					Count:           len(rows) - expiredCountOf(rows),
					Items:           upLines,
					Expired:         exLines,
					ExpiredCount:    expiredCountOf(rows),
					UpcomingSection: upSection,
					ExpiredSection:  exSection,
					Device:          rep.Device,
					Days:            rep.Days,
				}
				msg := template.RenderAny(x.Template, rep.Device, rep.Days, items, windowSummary)
				title := renderMilestoneTitle(x.TitleTemplate, rep.Device, rep.Days, len(windowHits))
				for _, tg := range targets {
					key := fmt.Sprintf("mw:%d:%s:%s", x.ID, fireKey, tg.Recipient)
					if err := s.enqueue(ctx, key, device{ID: rep.ID}, x.Name, trigger, tg, title, msg, x.Format); err != nil {
						return err
					}
				}
				continue
			}
			// The per-device reminders. A device that reached one of the
			// offsets today is reminded; a scan that skipped a day reminds
			// every device that crossed an offset since the last scan.
			type milestoneHit struct {
				d      device
				days   int
				offset int
			}
			hits := []milestoneHit{}
			for _, d := range applicableDevices(x.ID) {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				for _, offset := range offsets {
					eligible := days == offset
					if previousScan != "" && previousScan < today {
						previousDays, e := remaining(d.End, previousScan)
						if e != nil {
							return e
						}
						eligible = previousDays > offset && days <= offset
					}
					if !eligible {
						continue
					}
					hits = append(hits, milestoneHit{d: d, days: days, offset: offset})
				}
			}
			for _, h := range hits {
				for _, tg := range targets {
					key := fmt.Sprintf("m:%d:%d:%s:%d:%s", x.ID, h.d.ID, h.d.End, h.offset, tg.Recipient)
					if err := s.enqueue(ctx, key, h.d, x.Name, fmt.Sprintf("%d 天", h.offset), tg, renderTitle(x.TitleTemplate, h.d, h.days), render(x.Template, h.d, h.days), x.Format); err != nil {
						return err
					}
				}
			}
		case "cycle":
			if x.Interval < 1 {
				continue
			}
			for _, d := range applicableDevices(x.ID) {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				if days > x.Threshold {
					continue
				}
				bucket := (x.Threshold - days) / x.Interval
				for _, tg := range targets {
					key := fmt.Sprintf("c:%d:%d:%s:%d:%s", x.ID, d.ID, d.End, bucket, tg.Recipient)
					if e = s.enqueue(ctx, key, d, x.Name, fmt.Sprintf("周期第 %d 次", bucket+1), tg, renderTitle(x.TitleTemplate, d, days), render(x.Template, d, days), x.Format); e != nil {
						return e
					}
				}
			}
		case "monthly":
			// The summary fires on its cron schedule when one is set, or on
			// the monthly day of the strategy otherwise. The dedupe key
			// carries the minute the strategy fired on, so a schedule that
			// fires several times a day sends one message per fire, while
			// the same fire on a repeated scan does not repeat the message.
			var fireKey string
			if strings.TrimSpace(x.CronExpr) != "" {
				sched, e := cron.Parse(x.CronExpr)
				if e != nil {
					return fmt.Errorf("策略 %s 的 cron 表达式无效: %w", x.Name, e)
				}
				if !sched.Matches(now) {
					continue
				}
				fireKey = now.Format("2006-01-02T15:04")
			} else {
				if now.Day() < x.Day {
					continue
				}
				fireKey = now.Format("2006-01")
			}
			// Every device of the horizon is collected first, so the blocks
			// can be ordered by urgency before they are laid out. A fire
			// without devices sends no summary at all, because an empty
			// list tells the reader nothing.
			rows := []template.SummaryRow{}
			for _, d := range applicableDevices(x.ID) {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				if days < 0 || days <= x.Horizon {
					rows = append(rows, template.SummaryRow{Device: d.asTemplateDevice(), Days: days})
				}
			}
			if len(rows) == 0 {
				continue
			}
			// The representative device carries the device markers of the
			// title: the most urgent one of the summary.
			first := rows[0]
			for _, r := range rows[1:] {
				a, b := absDays(r.Days), absDays(first.Days)
				if a < b || (a == b && r.Device.End < first.Device.End) {
					first = r
				}
			}
			upcomingSection, expiredSection := template.BuildSummaryBlocks(x.Format, x.ItemTemplate, rows)
			upcomingLines, expiredLines := template.BuildSummaryLines(x.ItemTemplate, rows)
			for _, tg := range targets {
				summary := template.Summary{
					Month:           now.Format("2006-01"),
					Count:           len(rows) - expiredCountOf(rows),
					Items:           upcomingLines,
					Expired:         expiredLines,
					ExpiredCount:    expiredCountOf(rows),
					UpcomingSection: upcomingSection,
					ExpiredSection:  expiredSection,
					Device:          first.Device,
					Days:            first.Days,
				}
				// A summary trigger may also carry a template written for
				// another kind, so every marker the renderer knows is
				// filled: the grouped list comes from the same devices.
				hits := make([]template.MilestoneHit, 0, len(rows))
				for _, r := range rows {
					hits = append(hits, template.MilestoneHit{Device: r.Device, Days: r.Days})
				}
				items := template.BuildMilestoneItems(x.Format, []int{90, 60, 30, 7, 0}, hits)
				msg := template.RenderAny(x.Template, first.Device, first.Days, items, summary)
				title := template.RenderTitleSummary(x.TitleTemplate, summary)
				key := fmt.Sprintf("month:%d:%s:%s", x.ID, fireKey, tg.Recipient)
				if err = s.enqueue(ctx, key, device{}, x.Name, "定时汇总", tg, title, msg, x.Format); err != nil {
					return err
				}
			}
		}
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('scheduler.last_scan_date',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, today)
	return err
}

func (s *Scheduler) devices(ctx context.Context) ([]device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,p.name,p.customer,p.contact,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner),d.strategy_override FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []device{}
	for rows.Next() {
		var d device
		if err = rows.Scan(&d.ID, &d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &d.Start, &d.End, &d.Notes, &d.Owner, &d.StrategyOverride); err != nil {
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

// target is one resolved delivery destination: the recipient label plus the
// token and topic that will be used to send.
type target struct {
	Recipient string
	Token     push.Token
	Topic     string
}

// targets resolves the recipients of a strategy into concrete delivery
// targets. A token bound to the account wins over the token chosen on the
// strategy, which in turn wins over the global default. Group tokens address
// their group code instead of the account.
func (s *Scheduler) targets(ctx context.Context, strategyTokenID int64, recipients []string) ([]target, error) {
	store := push.NewStore(s.DB)
	out := []target{}
	seen := map[string]bool{}
	for _, name := range recipients {
		token, err := store.ForUser(name)
		if err != nil || !token.Enabled {
			token = push.Token{}
			if strategyTokenID > 0 {
				if t, e := store.Get(strategyTokenID); e == nil && t.Enabled {
					token = t
				}
			}
			if token.ID == 0 {
				if t, e := store.Default(); e == nil {
					token = t
				}
			}
		}
		if token.ID == 0 && s.globalToken(ctx) == "" {
			continue
		}
		recipient := name
		topic := ""
		if token.Kind == push.KindGroup && token.TopicCode != "" {
			topic = token.TopicCode
			recipient = token.TopicCode
		}
		key := recipient + "|" + topic
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, target{Recipient: recipient, Token: token, Topic: topic})
	}
	return out, nil
}
func (s *Scheduler) globalToken(ctx context.Context) string {
	var token string
	if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='pushplus_token'`).Scan(&token) == nil && token != "" {
		return token
	}
	return s.Config.PushPlusToken
}
func (s *Scheduler) enqueue(ctx context.Context, key string, d device, strategy, trigger string, tg target, title, msg, format string) error {
	channel := tg.Token.Channel
	if channel == "" {
		channel = "wechat"
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO notification_outbox(dedupe_key,device_id,project_name,strategy_name,trigger_point,recipient,title,message,channel,token_id,token_name,channel_detail,format) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING`, key, nullID(d.ID), d.Project, strategy, trigger, tg.Recipient, title, msg, channel, nullID(tg.Token.ID), tg.Token.Name, tg.Topic, format)
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

// asTemplateDevice turns a device record into the record the template
// renderer works with.
func (d device) asTemplateDevice() template.Device {
	return template.Device{
		Project:  d.Project,
		Customer: d.Customer,
		Contact:  d.Contact,
		Vendor:   d.Vendor,
		Model:    d.Model,
		SN:       d.SN,
		Start:    d.Start,
		End:      d.End,
		Notes:    d.Notes,
		Owner:    d.Owner,
	}
}

func render(t string, d device, days int) string {
	return template.Render(t, d.asTemplateDevice(), days)
}

// renderTitle renders the title of a reminder. An empty title template falls
// back to the default title.
func renderTitle(t string, d device, days int) string {
	if strings.TrimSpace(t) == "" {
		return DefaultTitle
	}
	return template.RenderTitle(t, template.Device{
		Project:  d.Project,
		Customer: d.Customer,
		Contact:  d.Contact,
		Vendor:   d.Vendor,
		Model:    d.Model,
		SN:       d.SN,
		Start:    d.Start,
		End:      d.End,
		Notes:    d.Notes,
		Owner:    d.Owner,
	}, days)
}

// renderItem lays out one line of the monthly summary. An empty item
// template falls back to the built-in layout.
func renderItem(t string, d device, days int) string {
	if strings.TrimSpace(t) == "" {
		t = DefaultItemTemplate
	}
	return template.RenderItem(t, template.Device{
		Project:  d.Project,
		Customer: d.Customer,
		Contact:  d.Contact,
		Vendor:   d.Vendor,
		Model:    d.Model,
		SN:       d.SN,
		Start:    d.Start,
		End:      d.End,
		Notes:    d.Notes,
		Owner:    d.Owner,
	}, days)
}

// expiredCountOf counts the devices of a summary that already expired.
func expiredCountOf(rows []template.SummaryRow) int {
	n := 0
	for _, r := range rows {
		if r.Days < 0 {
			n++
		}
	}
	return n
}

// absDays returns the distance to the end date, so that the devices of a
// summary can be ordered by how close they are to it.
func absDays(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// renderMilestoneTitle renders the title of a milestone message. A title
// may name the number of devices the message covers, so {{count}} is
// filled with it; the other markers come from the representative device.
func renderMilestoneTitle(t string, d template.Device, days, count int) string {
	if strings.TrimSpace(t) == "" {
		return DefaultTitle
	}
	values := map[string]string{
		"project":  d.Project,
		"customer": d.Customer,
		"contact":  d.Contact,
		"vendor":   d.Vendor,
		"model":    d.Model,
		"sn":       d.SN,
		"start":    d.Start,
		"end":      d.End,
		"days":     fmt.Sprint(days),
		"notes":    d.Notes,
		"owner":    d.Owner,
		"device":   strings.TrimSpace(d.Vendor + " " + d.Model),
		"month":    "",
		"count":    fmt.Sprint(count),
	}
	return template.ReplaceKeys(t, values)
}

func (s *Scheduler) Deliver(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,recipient,title,message,COALESCE(token_id,0),attempts,format,strategy_name,trigger_point,project_name,token_name,channel_detail FROM notification_outbox WHERE status IN ('pending','failed') AND next_attempt<=CURRENT_TIMESTAMP AND (attempts < ? OR last_error NOT LIKE '已达到重试上限%') ORDER BY id LIMIT 50`, retryLimitOf(s.pushDefaults(ctx)["retry_limit"]))
	if err != nil {
		return err
	}
	items := []outboxItem{}
	for rows.Next() {
		var x outboxItem
		if err = rows.Scan(&x.ID, &x.To, &x.Title, &x.Message, &x.TokenID, &x.Attempts, &x.Format, &x.StrategyName, &x.Trigger, &x.Project, &x.TokenName, &x.Topic); err != nil {
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
	store := push.NewStore(s.DB)
	defaults := s.pushDefaults(ctx)
	client := s.clientWithTimeout(timeoutSeconds(defaults["timeout_seconds"]))
	retryLimit := retryLimitOf(defaults["retry_limit"])
	for _, x := range items {
		// A message that has already been tried as often as allowed is left
		// alone instead of being retried forever. It is parked far enough in
		// the future that the scan does not pick it up again straight away.
		if x.Attempts >= retryLimit {
			detail := fmt.Sprintf("已达到重试上限 %d 次，不再自动重试", retryLimit)
			if _, e := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET last_error=? WHERE id=? AND attempts>=? AND status IN ('pending','failed')`, detail, x.ID, retryLimit); e != nil {
				return e
			}
			s.logOutbox(x, "push_abandoned", systemlog.LevelError, detail)
			continue
		}
		leaseSeconds := int64(timeoutSeconds(defaults["timeout_seconds"]) / time.Second)
		leaseSeconds = leaseSeconds*50 + 60
		result, e := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sending',attempts=attempts+1,next_attempt=datetime('now','+' || ? || ' seconds') WHERE id=? AND status IN ('pending','failed') AND attempts < ?`, leaseSeconds, x.ID, retryLimit)
		if e != nil {
			return e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			continue
		}
		token, topic := s.resolveToken(ctx, store, x.To, x.TokenID)
		// The format of the strategy decides how the message is written, so
		// it wins over the template of the token. A message that was queued
		// before the format existed falls back to the token and the default.
		pushTemplate := firstNonEmpty(x.Format, token.Template, defaults["template"])
		msg := push.Message{
			Token:       token.Token,
			Title:       firstNonEmpty(x.Title, DefaultTitle),
			Content:     x.Message,
			Template:    pushTemplate,
			Channel:     pick(token.Channel, defaults["channel"]),
			Option:      pick(token.Option, defaults["option"]),
			CallbackURL: pick(token.CallbackURL, defaults["callback_url"]),
			Topic:       topic,
		}
		if token.Token == "" {
			msg.Token = s.globalToken(ctx)
		}
		res, sendErr := client.Send(ctx, msg)
		success := sendErr == nil
		detail := ""
		code := ""
		if sendErr != nil {
			detail = sendErr.Error()
			var apiErr *push.APIError
			if errors.As(sendErr, &apiErr) {
				code = strconv.Itoa(apiErr.Code)
			}
		}
		_, attemptErr := s.DB.ExecContext(ctx, `INSERT INTO notification_attempts(outbox_id,succeeded,detail,code) VALUES(?,?,?,?)`, x.ID, boolInt(success), detail, code)
		if attemptErr != nil {
			return attemptErr
		}
		if success {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sent',sent_at=CURRENT_TIMESTAMP,last_error='',msg_id=?,token_name=? WHERE id=?`, res.MsgID, token.Name, x.ID)
		} else {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='failed',last_error=?,token_name=?,next_attempt=datetime('now', '+' || MIN(attempts*5,360) || ' minutes') WHERE id=?`, detail, token.Name, x.ID)
		}
		if e != nil {
			return e
		}
		x.Attempts++
		x.TokenName = token.Name
		x.Channel = msg.Channel
		x.Topic = topic
		x.Code = code
		x.MsgID = res.MsgID
		s.logDeliver(x, msg, success, detail)
	}
	return nil
}

type outboxItem struct {
	ID                                                                     int64
	To, Title, Message                                                     string
	TokenID                                                                int64
	Attempts                                                               int
	Format                                                                 string
	StrategyName, Trigger, Project, TokenName, Channel, Topic, Code, MsgID string
}

// logDeliver records one delivery attempt in the system log.
func (s *Scheduler) logDeliver(x outboxItem, msg push.Message, success bool, detail string) {
	if s.Log == nil {
		return
	}
	level := systemlog.LevelInfo
	action := "push_sent"
	if !success {
		level, action = systemlog.LevelError, "push_failed"
	}
	x.Channel = msg.Channel
	text := fmt.Sprintf("收件人：%s\n项目：%s\n策略：%s\n触发点：%s\n渠道：%s\n令牌：%s\n尝试次数：%d\n流水号：%s\n错误码：%s\n标题：%s\n消息：%s\n结果：%s", x.To, x.Project, x.StrategyName, x.Trigger, x.Channel, x.TokenName, x.Attempts, x.MsgID, x.Code, msg.Title, msg.Content, detail)
	message := "推送已发送：" + msg.Title
	if !success {
		message = "推送失败：" + msg.Title
	}
	_ = s.Log.Record(systemlog.Entry{Level: level, Category: systemlog.CategoryPush, Action: action, Message: message, Detail: text, RefType: "outbox", RefID: x.ID})
}

func (s *Scheduler) logOutbox(x outboxItem, action, level, detail string) {
	if s.Log == nil {
		return
	}
	message := fmt.Sprintf("%s：%s", action, firstNonEmpty(x.Title, DefaultTitle))
	text := fmt.Sprintf("收件人：%s\n项目：%s\n策略：%s\n触发点：%s\n渠道：%s\n令牌：%s\n尝试次数：%d\n流水号：%s\n消息：%s\n结果：%s", x.To, x.Project, x.StrategyName, x.Trigger, x.Channel, x.TokenName, x.Attempts, x.MsgID, x.Message, detail)
	_ = s.Log.Record(systemlog.Entry{Level: level, Category: systemlog.CategoryPush, Action: action, Message: message, Detail: text, RefType: "outbox", RefID: x.ID})
}

// clientWithTimeout returns the push client with the request timeout applied.
// The configured client is kept so its endpoint stays overridable.
func (s *Scheduler) clientWithTimeout(timeout time.Duration) *push.Client {
	base := s.Push
	if base == nil {
		base = push.NewClient()
	}
	endpoint := base.Endpoint
	if endpoint == "" {
		endpoint = push.Endpoint
	}
	return &push.Client{HTTP: &http.Client{Timeout: timeout}, Endpoint: endpoint}
}

// timeoutSeconds reads the configured request timeout, falling back to the
// documented default when the value is missing or not a number.
func timeoutSeconds(raw string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		n = int(push.DefaultTimeout / time.Second)
	}
	return time.Duration(n) * time.Second
}

// retryLimitOf reads the configured number of attempts, falling back to the
// documented default when the value is missing or not a number.
func retryLimitOf(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 6
	}
	return n
}

// resolveToken picks the token for a queued message: the token recorded when
// the message was enqueued wins, otherwise the account binding, otherwise the
// global default. A group token supplies the topic for the message.
func (s *Scheduler) resolveToken(ctx context.Context, store *push.Store, recipient string, tokenID int64) (push.Token, string) {
	if tokenID > 0 {
		if t, err := store.Get(tokenID); err == nil && t.Enabled {
			return t, groupTopic(t)
		}
		return push.Token{}, ""
	}
	if t, err := store.ForUser(recipient); err == nil && t.Enabled {
		return t, groupTopic(t)
	}
	if t, err := store.Default(); err == nil {
		return t, groupTopic(t)
	}
	return push.Token{}, ""
}

func groupTopic(t push.Token) string {
	if t.Kind == push.KindGroup {
		return t.TopicCode
	}
	return ""
}

// pushDefaults reads the global push settings, falling back to the documented
// defaults when a key is missing. The template falls back to markdown,
// because the messages default to that format.
func (s *Scheduler) pushDefaults(ctx context.Context) map[string]string {
	out := map[string]string{"template": template.FormatMarkdown, "channel": "wechat", "timeout_seconds": "15", "retry_limit": "6"}
	rows, err := s.DB.QueryContext(ctx, `SELECT key,value FROM settings WHERE key LIKE 'push.%'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return out
		}
		out[strings.TrimPrefix(k, "push.")] = v
	}
	return out
}

func (s *Scheduler) globalTimezone(ctx context.Context) string {
	var timezone string
	if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='timezone'`).Scan(&timezone) == nil && timezone != "" {
		return timezone
	}
	return s.Config.Timezone
}

func pick(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
