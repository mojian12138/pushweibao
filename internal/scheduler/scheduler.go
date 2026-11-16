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
}
type strategy struct {
	ID                                                                     int64
	Name, Type, Offsets, Template, TitleTemplate, ItemTemplate, Recipients string
	Day, Horizon, Threshold, Interval                                      int
	TokenID                                                                int64
	// Format is the way the message is written: html, txt or markdown. It
	// also decides the PushPlus template the message is sent with.
	Format string
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,type,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,title_template,item_template,recipients,COALESCE(token_id,0),format FROM strategies WHERE enabled=1 ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	strategies := []strategy{}
	for rows.Next() {
		var x strategy
		if err = rows.Scan(&x.ID, &x.Name, &x.Type, &x.Offsets, &x.Day, &x.Horizon, &x.Threshold, &x.Interval, &x.Template, &x.TitleTemplate, &x.ItemTemplate, &x.Recipients, &x.TokenID, &x.Format); err != nil {
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
			for _, d := range devices {
				days, e := remaining(d.End, today)
				if e != nil {
					return e
				}
				for _, offset := range offsets {
					if days == offset {
						for _, tg := range targets {
							key := fmt.Sprintf("m:%d:%d:%s:%d:%s", x.ID, d.ID, d.End, offset, tg.Recipient)
							if e = s.enqueue(ctx, key, d, x.Name, fmt.Sprintf("%d 天", offset), tg, renderTitle(x.TitleTemplate, d, days), render(x.Template, d, days), x.Format); e != nil {
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
				for _, tg := range targets {
					key := fmt.Sprintf("c:%d:%d:%s:%d:%s", x.ID, d.ID, d.End, bucket, tg.Recipient)
					if e = s.enqueue(ctx, key, d, x.Name, fmt.Sprintf("周期第 %d 次", bucket+1), tg, renderTitle(x.TitleTemplate, d, days), render(x.Template, d, days), x.Format); e != nil {
						return e
					}
				}
			}
		case "monthly":
			if now.Day() != x.Day {
				continue
			}
			monthKey := now.Format("2006-01")
			for _, tg := range targets {
				var upcoming, expired strings.Builder
				upcomingCount, expiredCount := 0, 0
				var first device
				firstDays := 0
				for _, d := range devices {
					days, e := remaining(d.End, today)
					if e != nil {
						return e
					}
					line := renderItem(x.ItemTemplate, d, days)
					if days < 0 {
						expired.WriteString(line)
						expiredCount++
					} else if days <= x.Horizon {
						upcoming.WriteString(line)
						upcomingCount++
					}
					// The first device is the representative one, so that a
					// title may use the device markers as well.
					if first.End == "" {
						first, firstDays = d, days
					}
				}
				summary := template.Summary{
					Month:        monthKey,
					Count:        upcomingCount,
					Items:        upcoming.String(),
					Expired:      expired.String(),
					ExpiredCount: expiredCount,
					Device:       first.asTemplateDevice(),
					Days:         firstDays,
				}
				msg := template.RenderSummary(x.Template, summary)
				title := template.RenderTitleSummary(x.TitleTemplate, summary)
				key := fmt.Sprintf("month:%d:%s:%s", x.ID, monthKey, tg.Recipient)
				if err = s.enqueue(ctx, key, device{}, x.Name, "月度汇总", tg, title, msg, x.Format); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Scheduler) devices(ctx context.Context) ([]device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,p.name,p.customer,p.contact,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner) FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.status='active'`)
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
		if token.ID == 0 && s.Config.PushPlusToken == "" {
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

func (s *Scheduler) Deliver(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,recipient,title,message,COALESCE(token_id,0),attempts,format FROM notification_outbox WHERE status IN ('pending','failed') AND next_attempt<=CURRENT_TIMESTAMP ORDER BY id LIMIT 50`)
	if err != nil {
		return err
	}
	items := []outboxItem{}
	for rows.Next() {
		var x outboxItem
		if err = rows.Scan(&x.ID, &x.To, &x.Title, &x.Message, &x.TokenID, &x.Attempts, &x.Format); err != nil {
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
			_, _ = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='failed',last_error=?,next_attempt=datetime('now','+1 day') WHERE id=?`, fmt.Sprintf("已达到重试上限 %d 次，不再自动重试", retryLimit), x.ID)
			continue
		}
		result, e := s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sending',attempts=attempts+1 WHERE id=? AND status IN ('pending','failed')`, x.ID)
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
			msg.Token = s.Config.PushPlusToken
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
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO notification_attempts(outbox_id,succeeded,detail,code) VALUES(?,?,?,?)`, x.ID, boolInt(success), detail, code)
		if success {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='sent',sent_at=CURRENT_TIMESTAMP,last_error='',msg_id=?,token_name=? WHERE id=?`, res.MsgID, token.Name, x.ID)
		} else {
			_, e = s.DB.ExecContext(ctx, `UPDATE notification_outbox SET status='failed',last_error=?,token_name=?,next_attempt=datetime('now', '+' || MIN(attempts*5,360) || ' minutes') WHERE id=?`, detail, token.Name, x.ID)
		}
		if e != nil {
			return e
		}
		// The attempt is written to the log, so that the push history can be
		// seen even when the message list is not shown.
		s.logDeliver(x, msg, success, detail)
	}
	return nil
}

// outboxItem is one queued message that is waiting to be sent.
type outboxItem struct {
	ID                 int64
	To, Title, Message string
	TokenID            int64
	Attempts           int
	Format             string
}

// logDeliver records one delivery attempt in the system log.
func (s *Scheduler) logDeliver(x outboxItem, msg push.Message, success bool, detail string) {
	if s.Log == nil {
		return
	}
	level := systemlog.LevelInfo
	action := "push_sent"
	message := "推送已发送：" + msg.Title
	text := msg.Content
	if !success {
		level = systemlog.LevelError
		action = "push_failed"
		message = "推送失败：" + msg.Title
		text = msg.Content + "｜错误：" + detail
	}
	_ = s.Log.Record(systemlog.Entry{
		Level:    level,
		Category: systemlog.CategoryPush,
		Action:   action,
		Message:  message,
		Detail:   text,
		RefType:  "outbox",
		RefID:    x.ID,
	})
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
		if t, err := store.Get(tokenID); err == nil {
			return t, groupTopic(t)
		}
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
// defaults when a key is missing.
func (s *Scheduler) pushDefaults(ctx context.Context) map[string]string {
	out := map[string]string{"template": "html", "channel": "wechat", "timeout_seconds": "15", "retry_limit": "6"}
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
