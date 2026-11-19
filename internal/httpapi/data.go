package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/auth"
	"maintenance-reminder/internal/cron"
	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/systemlog"
	"maintenance-reminder/internal/template"
)

type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Customer    string `json:"customer"`
	Contact     string `json:"contact"`
	Notes       string `json:"notes"`
	ContractNo  string `json:"contract_no"`
	Owner       string `json:"owner"`
	DeviceCount int    `json:"device_count,omitempty"`
	NearestDue  string `json:"nearest_due,omitempty"`
}
type Device struct {
	ID               int64  `json:"id"`
	ProjectID        FlexID `json:"project_id"`
	Project          string `json:"project,omitempty"`
	Customer         string `json:"customer,omitempty"`
	Vendor           string `json:"vendor"`
	Model            string `json:"model"`
	Serial           string `json:"serial_number"`
	Start            string `json:"maintenance_start"`
	End              string `json:"maintenance_end"`
	Status           string `json:"status"`
	Notes            string `json:"notes"`
	Owner            string `json:"owner"`
	ContractNo       string `json:"contract_no"`
	StrategyOverride int    `json:"strategy_override"`
	Remaining        int    `json:"remaining_days"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.db.Query(`SELECT p.id,p.name,p.customer,p.contact,p.notes,p.contract_no,p.owner,COUNT(d.id),COALESCE(MIN(CASE WHEN d.status='active' THEN d.maintenance_end END),'') FROM projects p LEFT JOIN devices d ON d.project_id=p.id WHERE (?='' OR p.name LIKE ? OR p.customer LIKE ? OR p.contact LIKE ?) GROUP BY p.id ORDER BY p.name`, q, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Name, &p.Customer, &p.Contact, &p.Notes, &p.ContractNo, &p.Owner, &p.DeviceCount, &p.NearestDue); err != nil {
			errorResponse(w, err)
			return
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		errorResponse(w, err)
		return
	}
	ok(w, out)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var p Project
	if decode(r, &p) != nil || strings.TrimSpace(p.Name) == "" {
		fail(w, 400, "VALIDATION", "项目名必填")
		return
	}
	res, err := s.db.Exec(`INSERT INTO projects(name,customer,contact,notes,contract_no,owner) VALUES(?,?,?,?,?,?)`, strings.TrimSpace(p.Name), p.Customer, p.Contact, p.Notes, p.ContractNo, p.Owner)
	if err != nil {
		errorResponse(w, err)
		return
	}
	p.ID, _ = res.LastInsertId()
	s.auditMutation(r, systemlog.CategoryData, "project_created", "project", p.ID, fmt.Sprintf("项目=%s 客户=%s 联系人=%s 负责人=%s", p.Name, p.Customer, p.Contact, p.Owner))
	writeJSON(w, 201, envelope{OK: true, Data: p})
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var p Project
	if e != nil || decode(r, &p) != nil || strings.TrimSpace(p.Name) == "" {
		fail(w, 400, "VALIDATION", "项目数据无效")
		return
	}
	res, err := s.db.Exec(`UPDATE projects SET name=?,customer=?,contact=?,notes=?,contract_no=?,owner=? WHERE id=?`, p.Name, p.Customer, p.Contact, p.Notes, p.ContractNo, p.Owner, id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "项目不存在")
		return
	}
	p.ID = id
	s.auditMutation(r, systemlog.CategoryData, "project_updated", "project", id, fmt.Sprintf("项目=%s 客户=%s 联系人=%s 负责人=%s", p.Name, p.Customer, p.Contact, p.Owner))
	ok(w, p)
}
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	deleteByID(w, r, s, "projects", "项目")
}
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	project := r.URL.Query().Get("project_id")
	status := r.URL.Query().Get("status")
	bucket := r.URL.Query().Get("bucket")
	vendor := r.URL.Query().Get("vendor")
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	today := time.Now().In(mustLocation(s.timezone())).Format("2006-01-02")
	query := `SELECT d.id,d.project_id,p.name,p.customer,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.status,d.notes,d.owner,d.contract_no,d.strategy_override,CAST(julianday(d.maintenance_end)-julianday(?) AS INTEGER) FROM devices d JOIN projects p ON p.id=d.project_id WHERE 1=1`
	args := []any{today}
	if project != "" {
		query += ` AND d.project_id=?`
		args = append(args, project)
	}
	if status != "" {
		query += ` AND d.status=?`
		args = append(args, status)
	}
	if vendor != "" {
		query += ` AND d.vendor LIKE ?`
		args = append(args, "%"+vendor+"%")
	}
	if from != "" {
		query += ` AND d.maintenance_end>=?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND d.maintenance_end<=?`
		args = append(args, to)
	}
	switch bucket {
	case "expired":
		query += ` AND d.status='active' AND d.maintenance_end<?`
		args = append(args, today)
	case "soon30":
		query += ` AND d.status='active' AND d.maintenance_end>=? AND d.maintenance_end<=date(?,'+30 day')`
		args = append(args, today, today)
	case "soon90":
		query += ` AND d.status='active' AND d.maintenance_end>=? AND d.maintenance_end<=date(?,'+90 day')`
		args = append(args, today, today)
	case "active":
		query += ` AND d.status='active' AND d.maintenance_end>=?`
		args = append(args, today)
	}
	if q != "" {
		query += ` AND (d.vendor LIKE ? OR d.model LIKE ? OR d.serial_number LIKE ? OR p.name LIKE ? OR p.customer LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like)
	}
	query += ` ORDER BY d.maintenance_end,d.id`
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err = rows.Scan(&d.ID, (*int64)(&d.ProjectID), &d.Project, &d.Customer, &d.Vendor, &d.Model, &d.Serial, &d.Start, &d.End, &d.Status, &d.Notes, &d.Owner, &d.ContractNo, &d.StrategyOverride, &d.Remaining); err != nil {
			errorResponse(w, err)
			return
		}
		out = append(out, d)
	}
	if err = rows.Err(); err != nil {
		errorResponse(w, err)
		return
	}
	ok(w, out)
}
func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var d Device
	if decode(r, &d) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if err := validateDevice(d); err != nil {
		fail(w, 400, "VALIDATION", err.Error())
		return
	}
	res, err := s.db.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,notes,owner,contract_no,strategy_override) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, d.ProjectID, d.Vendor, d.Model, d.Serial, d.Start, d.End, defaultStatus(d.Status), d.Notes, d.Owner, d.ContractNo, d.StrategyOverride)
	if err != nil {
		errorResponse(w, err)
		return
	}
	d.ID, _ = res.LastInsertId()
	s.auditMutation(r, systemlog.CategoryData, "device_created", "device", d.ID, fmt.Sprintf("项目ID=%d 厂商=%s 型号=%s SN=%s 到期=%s 状态=%s", d.ProjectID, d.Vendor, d.Model, d.Serial, d.End, defaultStatus(d.Status)))
	writeJSON(w, 201, envelope{OK: true, Data: d})
}

// getDevice returns one device with its project, so that the test push can
// show the data of that device.
func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var d Device
	var projectID int64
	e = s.db.QueryRow(`SELECT d.id,d.project_id,p.name,p.customer,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.status,d.notes,d.owner,d.contract_no,d.strategy_override FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.id=?`, id).Scan(&d.ID, &projectID, &d.Project, &d.Customer, &d.Vendor, &d.Model, &d.Serial, &d.Start, &d.End, &d.Status, &d.Notes, &d.Owner, &d.ContractNo, &d.StrategyOverride)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "设备不存在")
		return
	}
	d.ProjectID = FlexID(projectID)
	d.Remaining = s.daysUntil(d.End)
	ok(w, d)
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var d Device
	if e != nil || decode(r, &d) != nil {
		fail(w, 400, "BAD_REQUEST", "设备数据无效")
		return
	}
	if err := validateDevice(d); err != nil {
		fail(w, 400, "VALIDATION", err.Error())
		return
	}
	res, err := s.db.Exec(`UPDATE devices SET project_id=?,vendor=?,model=?,serial_number=?,maintenance_start=?,maintenance_end=?,status=?,notes=?,owner=?,contract_no=?,strategy_override=? WHERE id=?`, d.ProjectID, d.Vendor, d.Model, d.Serial, d.Start, d.End, defaultStatus(d.Status), d.Notes, d.Owner, d.ContractNo, d.StrategyOverride, id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "设备不存在")
		return
	}
	d.ID = id
	s.auditMutation(r, systemlog.CategoryData, "device_updated", "device", id, fmt.Sprintf("项目ID=%d 厂商=%s 型号=%s SN=%s 到期=%s 状态=%s 策略覆盖=%d", d.ProjectID, d.Vendor, d.Model, d.Serial, d.End, defaultStatus(d.Status), d.StrategyOverride))
	ok(w, d)
}
func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	deleteByID(w, r, s, "devices", "设备")
}
func deleteByID(w http.ResponseWriter, r *http.Request, s *Server, table, label string) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	objectName := ""
	if table == "projects" {
		_ = s.db.QueryRow(`SELECT name FROM projects WHERE id=?`, id).Scan(&objectName)
	}
	if table == "devices" {
		_ = s.db.QueryRow(`SELECT serial_number FROM devices WHERE id=?`, id).Scan(&objectName)
	}
	res, err := s.db.Exec(`DELETE FROM `+table+` WHERE id=?`, id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", label+"不存在")
		return
	}
	s.auditMutation(r, systemlog.CategoryData, "record_deleted", strings.ToLower(label), id, fmt.Sprintf("%s=%s", label, objectName))
	ok(w, map[string]bool{"deleted": true})
}

// FlexID accepts a JSON number or a numeric string (the HTML form sends strings).
type FlexID int64

func (f *FlexID) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case float64:
		*f = FlexID(int64(v))
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return fmt.Errorf("项目编号无效")
		}
		*f = FlexID(parsed)
	default:
		return fmt.Errorf("项目编号无效")
	}
	return nil
}

func validateDevice(d Device) error {
	if d.ProjectID <= 0 || strings.TrimSpace(d.Vendor) == "" || strings.TrimSpace(d.Model) == "" || strings.TrimSpace(d.Serial) == "" {
		return fmt.Errorf("项目、厂商、型号和 SN 必填")
	}
	a, e := parseFlexibleDate(d.Start)
	if e != nil {
		return fmt.Errorf("维保开始日期格式应为 YYYY-MM-DD 或 YYYY/M/D")
	}
	b, e := parseFlexibleDate(d.End)
	if e != nil || b.Before(a) {
		return fmt.Errorf("维保结束日期无效")
	}
	d.Start, d.End = a.Format("2006-01-02"), b.Format("2006-01-02")
	if d.Status != "" && d.Status != "active" && d.Status != "renewed" && d.Status != "retired" {
		return fmt.Errorf("设备状态无效")
	}
	return nil
}

func parseFlexibleDate(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02", "2006/1/2", "2006-1-2", "2006/01/02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("日期格式无效: %q", v)
}
func defaultStatus(v string) string {
	if v == "" {
		return "active"
	}
	return v
}
func mustLocation(name string) *time.Location {
	l, e := time.LoadLocation(name)
	if e != nil {
		return time.Local
	}
	return l
}

type summary struct {
	Expired  int              `json:"expired"`
	Within30 int              `json:"within_30_days"`
	Within90 int              `json:"within_90_days"`
	Active   int              `json:"maintenance_active"`
	Vendors  []map[string]any `json:"vendors"`
	Monthly  []map[string]any `json:"monthly"`
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	today := time.Now().In(mustLocation(s.timezone())).Format("2006-01-02")
	var v summary
	err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN maintenance_end<? THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN maintenance_end>=? AND maintenance_end<=date(?,'+30 day') THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN maintenance_end>=? AND maintenance_end<=date(?,'+90 day') THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN maintenance_end>=? AND status='active' THEN 1 ELSE 0 END),0) FROM devices WHERE status='active'`, today, today, today, today, today, today).Scan(&v.Expired, &v.Within30, &v.Within90, &v.Active)
	if err != nil {
		errorResponse(w, err)
		return
	}
	rows, err := s.db.Query(`SELECT vendor,COUNT(*) FROM devices WHERE status='active' GROUP BY vendor ORDER BY COUNT(*) DESC`)
	if err != nil {
		errorResponse(w, err)
		return
	}
	v.Vendors = []map[string]any{}
	for rows.Next() {
		var n string
		var c int
		if err = rows.Scan(&n, &c); err != nil {
			rows.Close()
			errorResponse(w, err)
			return
		}
		v.Vendors = append(v.Vendors, map[string]any{"name": n, "count": c})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		errorResponse(w, err)
		return
	}
	rows.Close()
	rows, err = s.db.Query(`SELECT substr(maintenance_end,1,7),COUNT(*) FROM devices WHERE status='active' AND maintenance_end>=? GROUP BY substr(maintenance_end,1,7) ORDER BY 1 LIMIT 12`, today)
	if err != nil {
		errorResponse(w, err)
		return
	}
	v.Monthly = []map[string]any{}
	for rows.Next() {
		var m string
		var c int
		if err = rows.Scan(&m, &c); err != nil {
			rows.Close()
			errorResponse(w, err)
			return
		}
		v.Monthly = append(v.Monthly, map[string]any{"month": m, "count": c})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		errorResponse(w, err)
		return
	}
	rows.Close()
	ok(w, v)
}

type Strategy struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Enabled        bool     `json:"enabled"`
	Sort           int      `json:"sort_order"`
	Offsets        []int    `json:"offsets"`
	MonthlyDay     int      `json:"monthly_day"`
	Horizon        int      `json:"horizon_days"`
	CycleThreshold int      `json:"cycle_threshold"`
	CycleInterval  int      `json:"cycle_interval"`
	Template       string   `json:"template"`
	TitleTemplate  string   `json:"title_template"`
	ItemTemplate   string   `json:"item_template"`
	Recipients     []string `json:"recipients"`
	TokenID        int64    `json:"token_id"`
	// Format is the way the message is written: html, txt or markdown. It
	// also decides which PushPlus template the message is sent with.
	Format string `json:"format"`
	// CronExpr is the schedule the strategy fires on. An empty expression
	// keeps the trigger of the type: the milestone fires when a device
	// reaches a node, the monthly one fires on its monthly day.
	CronExpr string `json:"cron_expr"`
	// Describe names the schedule in plain words, so the list can show it
	// without the interface parsing the expression again.
	Describe string `json:"cron_describe,omitempty"`
	// TemplateID points at the message template the strategy sends. The
	// words of the message live with the template, so the strategies stay
	// small: a name, a trigger and a template.
	TemplateID int64 `json:"template_id"`
	// TemplateName is the name of the template the strategy points at, for
	// the list to show.
	TemplateName string `json:"template_name,omitempty"`
	// Preview is the body rendered with the sample data, so that the list
	// shows the layout of the message instead of its source.
	Preview string `json:"preview"`
}

// strategySampleBody renders the body of a strategy with the sample data, so
// that the list can show the message the way it will look instead of the
// template source.
func strategySampleBody(s *Server, x Strategy) string {
	// The grouped list of a milestone message is laid out in the format of
	// the strategy, so the preview matches the layout of the phone.
	if x.Type == template.KindMilestone {
		content := template.SampleWithout(x.Type, x.Template, "milestone_items")
		return template.RenderMilestone(content, template.SampleMilestoneItems(x.Format), template.SampleDevice(), 30)
	}
	body := template.Sample(x.Type, x.Template)
	if x.Type != template.KindMonthly {
		return body
	}
	// The summary is built around the lines of the item template, so both
	// have to be rendered for the table to be filled.
	item := x.ItemTemplate
	if strings.TrimSpace(item) == "" {
		item = template.DefaultItemFor(x.Format)
	}
	line := template.SampleItem(item) + "\n"
	summary := template.Summary{
		Month: s.currentMonth(),
		Count: 2,
		Items: line + line,
	}
	// The section markers are filled with the sample blocks of the format,
	// so that a list preview of a summary shows the layout of the message.
	summary.UpcomingSection, summary.ExpiredSection = template.SampleSummarySections(x.Format, item)
	return template.RenderSummary(x.Template, summary)
}

func (s *Server) listStrategies(w http.ResponseWriter, r *http.Request) {
	// The words of a bound template travel with the strategy through one
	// LEFT JOIN. A query inside the row loop would need a second database
	// connection while the outer rows still hold the only one, and the
	// whole server would wait on itself.
	rows, e := s.db.Query(`SELECT s.id,s.name,s.type,s.enabled,s.sort_order,s.offsets,s.monthly_day,s.horizon_days,s.cycle_threshold,s.cycle_interval,s.template,s.title_template,s.item_template,s.recipients,COALESCE(s.token_id,0),s.format,COALESCE(s.cron_expr,''),COALESCE(s.template_id,0),
		COALESCE(t.name,''),COALESCE(t.kind,''),COALESCE(t.format,''),COALESCE(t.title_template,''),COALESCE(t.body_template,''),COALESCE(t.item_template,'')
		FROM strategies s LEFT JOIN push_templates t ON t.id=s.template_id ORDER BY s.sort_order,s.id`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	out := []Strategy{}
	for rows.Next() {
		var x Strategy
		var enabled int
		var offsets, recipients string
		var tName, tKind, tFormat, tTitle, tBody, tItem string
		if e = rows.Scan(&x.ID, &x.Name, &x.Type, &enabled, &x.Sort, &offsets, &x.MonthlyDay, &x.Horizon, &x.CycleThreshold, &x.CycleInterval, &x.Template, &x.TitleTemplate, &x.ItemTemplate, &recipients, &x.TokenID, &x.Format, &x.CronExpr, &x.TemplateID,
			&tName, &tKind, &tFormat, &tTitle, &tBody, &tItem); e != nil {
			errorResponse(w, e)
			return
		}
		if strings.TrimSpace(x.CronExpr) != "" {
			x.Describe = cron.Describe(x.CronExpr)
		}
		// A strategy that points at a living template sends the words of
		// that template, so the list shows them and the preview renders
		// them.
		if x.TemplateID > 0 && tName != "" {
			x.TemplateName = tName
			x.Type = tKind
			x.Format = template.NormalizeFormat(tFormat)
			x.TitleTemplate = tTitle
			x.Template = tBody
			x.ItemTemplate = tItem
		}
		x.Enabled = enabled == 1
		_ = json.Unmarshal([]byte(offsets), &x.Offsets)
		_ = json.Unmarshal([]byte(recipients), &x.Recipients)
		x.Format = template.NormalizeFormat(x.Format)
		x.Preview = template.RenderAsHTML(x.Format, strategySampleBody(s, x))
		out = append(out, x)
	}
	ok(w, out)
}
func (s *Server) saveStrategy(w http.ResponseWriter, r *http.Request, id int64, create bool) {
	var x Strategy
	if decode(r, &x) != nil || strings.TrimSpace(x.Name) == "" {
		fail(w, 400, "VALIDATION", "策略名称必填")
		return
	}
	if x.Type != "milestone" && x.Type != "monthly" && x.Type != "cycle" {
		fail(w, 400, "VALIDATION", "策略类型无效")
		return
	}
	// A strategy that points at a template sends the words of that
	// template, so the words are copied from it and the request does not
	// carry any of them.
	if x.TemplateID > 0 {
		var kind, format, title, body, item string
		if e := s.db.QueryRow(`SELECT kind,format,title_template,body_template,item_template FROM push_templates WHERE id=?`, x.TemplateID).Scan(&kind, &format, &title, &body, &item); e != nil {
			fail(w, 400, "VALIDATION", "所选模板不存在")
			return
		}
		x.Type = kind
		x.Format = format
		x.TitleTemplate = title
		x.Template = body
		x.ItemTemplate = item
	}
	// A template that uses a marker the type does not support is rejected
	// here, so that a message that would go out half empty never reaches
	// the queue.
	if e := template.Validate(x.Type, x.Template, false); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	if e := template.ValidateTitle(x.TitleTemplate); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	if x.ItemTemplate != "" {
		if e := template.ValidateItem(x.ItemTemplate); e != nil {
			fail(w, 400, "VALIDATION", e.Error())
			return
		}
	}
	// The schedule of a cron trigger has to be a readable expression, so a
	// broken one is refused here instead of failing the scan every time.
	x.CronExpr = strings.TrimSpace(x.CronExpr)
	if x.CronExpr != "" {
		if e := cron.Validate(x.CronExpr); e != nil {
			fail(w, 400, "VALIDATION", "触发计划无效："+e.Error())
			return
		}
	}
	// The format decides how the message is written, so an unknown one is
	// refused instead of being sent as plain text. A request that does not
	// name a format gets the default one, so that an old client keeps
	// working.
	if x.Format != "" && !template.ValidFormat(x.Format) {
		fail(w, 400, "VALIDATION", "消息格式无效，可选：html、txt、markdown")
		return
	}
	x.Format = template.NormalizeFormat(x.Format)
	// An empty message gets the ready-made template of the format, so that
	// every format works out of the box.
	if strings.TrimSpace(x.Template) == "" {
		x.Template = template.DefaultBodyFor(x.Format, x.Type)
	}
	if strings.TrimSpace(x.TitleTemplate) == "" {
		x.TitleTemplate = template.DefaultTitleFor(x.Format, x.Type)
	}
	if x.Type == "monthly" && strings.TrimSpace(x.ItemTemplate) == "" {
		x.ItemTemplate = template.DefaultItemFor(x.Format)
	}
	// The numbers a strategy needs have sensible defaults, so the editor of
	// a strategy only has to carry a name, a trigger and a template. The
	// offsets default to the five tiers, the horizon to the widest offset.
	if len(x.Offsets) == 0 {
		x.Offsets = []int{90, 60, 30, 7, 0}
	}
	if x.Horizon <= 0 {
		widest := 0
		for _, o := range x.Offsets {
			if o > widest {
				widest = o
			}
		}
		x.Horizon = widest
	}
	if x.MonthlyDay <= 0 {
		x.MonthlyDay = 1
	}
	if x.CycleThreshold <= 0 {
		x.CycleThreshold = 30
	}
	if x.CycleInterval <= 0 {
		x.CycleInterval = 7
	}
	off, _ := json.Marshal(x.Offsets)
	rec, _ := json.Marshal(x.Recipients)
	if create {
		res, e := s.db.Exec(`INSERT INTO strategies(name,type,enabled,sort_order,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,title_template,item_template,recipients,token_id,format,cron_expr,template_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, x.Name, x.Type, boolInt(x.Enabled), x.Sort, string(off), x.MonthlyDay, x.Horizon, x.CycleThreshold, x.CycleInterval, x.Template, x.TitleTemplate, x.ItemTemplate, string(rec), nullInt(x.TokenID), x.Format, x.CronExpr, x.TemplateID)
		if e != nil {
			errorResponse(w, e)
			return
		}
		x.ID, _ = res.LastInsertId()
		s.auditMutation(r, systemlog.CategoryStrategy, "strategy_created", "strategy", x.ID, fmt.Sprintf("策略=%s 类型=%s 启用=%t 接收人=%s", x.Name, x.Type, x.Enabled, strings.Join(x.Recipients, ",")))
	} else {
		res, e := s.db.Exec(`UPDATE strategies SET name=?,type=?,enabled=?,sort_order=?,offsets=?,monthly_day=?,horizon_days=?,cycle_threshold=?,cycle_interval=?,template=?,title_template=?,item_template=?,recipients=?,token_id=?,format=?,cron_expr=?,template_id=? WHERE id=?`, x.Name, x.Type, boolInt(x.Enabled), x.Sort, string(off), x.MonthlyDay, x.Horizon, x.CycleThreshold, x.CycleInterval, x.Template, x.TitleTemplate, x.ItemTemplate, string(rec), nullInt(x.TokenID), x.Format, x.CronExpr, x.TemplateID, id)
		if e != nil {
			errorResponse(w, e)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "NOT_FOUND", "策略不存在")
			return
		}
		x.ID = id
		s.auditMutation(r, systemlog.CategoryStrategy, "strategy_updated", "strategy", id, fmt.Sprintf("策略=%s 类型=%s 启用=%t 接收人=%s", x.Name, x.Type, x.Enabled, strings.Join(x.Recipients, ",")))
	}
	ok(w, x)
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// nullInt maps a zero id to NULL so the foreign key stays optional.
func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
func (s *Server) reorderStrategies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if decode(r, &req) != nil || len(req.IDs) == 0 {
		fail(w, 400, "VALIDATION", "策略顺序无效")
		return
	}
	seen := map[int64]bool{}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer tx.Rollback()
	var strategyCount int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM strategies`).Scan(&strategyCount); err != nil {
		errorResponse(w, err)
		return
	}
	if len(req.IDs) != strategyCount {
		fail(w, 400, "VALIDATION", "排序必须包含全部策略")
		return
	}
	for i, id := range req.IDs {
		if id <= 0 || seen[id] {
			fail(w, 400, "VALIDATION", "策略顺序包含无效或重复编号")
			return
		}
		seen[id] = true
		res, e := tx.ExecContext(r.Context(), `UPDATE strategies SET sort_order=? WHERE id=?`, (i+1)*10, id)
		if e != nil {
			errorResponse(w, e)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 400, "VALIDATION", "策略列表已变化，请刷新后重试")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		errorResponse(w, err)
		return
	}
	ok(w, map[string]bool{"reordered": true})
}
func (s *Server) createStrategy(w http.ResponseWriter, r *http.Request) {
	s.saveStrategy(w, r, 0, true)
}
func (s *Server) updateStrategy(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	s.saveStrategy(w, r, id, false)
}
func (s *Server) deleteStrategy(w http.ResponseWriter, r *http.Request) {
	deleteByID(w, r, s, "strategies", "策略")
}
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,project_name,strategy_name,trigger_point,channel,recipient,message,status,last_error,created_at,sent_at,token_name,channel_detail,msg_id FROM notification_outbox ORDER BY id DESC LIMIT 500`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	cols := []string{"id", "project", "strategy", "trigger", "channel", "recipient", "message", "status", "error", "created_at", "sent_at", "token_name", "channel_detail", "msg_id"}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			errorResponse(w, e)
			return
		}
		m := map[string]any{}
		for i, k := range cols {
			m[k] = vals[i]
		}
		out = append(out, m)
	}
	ok(w, out)
}
func (s *Server) retryNotification(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	// The attempt counter is reset as well, otherwise the automatic retry
	// limit would immediately park the message again without sending it.
	res, e := s.db.Exec(`UPDATE notification_outbox SET status='pending',next_attempt=CURRENT_TIMESTAMP,last_error='',attempts=0 WHERE id=? AND status!='sent'`, id)
	if e != nil {
		errorResponse(w, e)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "可重推记录不存在")
		return
	}
	s.auditMutation(r, systemlog.CategoryPush, "notification_retry_queued", "outbox", id, "管理员手动重试已排队")
	ok(w, map[string]bool{"queued": true})
}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT u.id,u.username,u.role,u.active,u.must_change_password,u.created_at,COALESCE(u.push_token_id,0),COALESCE(t.name,'') FROM users u LEFT JOIN push_tokens t ON t.id=u.push_token_id ORDER BY u.id`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, role, created, tokenName string
		var active, must int
		var tokenID int64
		if e = rows.Scan(&id, &name, &role, &active, &must, &created, &tokenID, &tokenName); e != nil {
			errorResponse(w, e)
			return
		}
		out = append(out, map[string]any{"id": id, "username": name, "role": role, "active": active == 1, "must_change_password": must == 1, "created_at": created, "push_token_id": tokenID, "push_token_name": tokenName})
	}
	ok(w, out)
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Token    string `json:"pushplus_token"`
		TokenID  int64  `json:"push_token_id"`
	}
	if decode(r, &x) != nil || strings.TrimSpace(x.Username) == "" {
		fail(w, 400, "VALIDATION", "账号名必填")
		return
	}
	if x.Role != "admin" && x.Role != "viewer" {
		fail(w, 400, "VALIDATION", "角色无效")
		return
	}
	hash, e := auth.HashPassword(x.Password)
	if e != nil {
		fail(w, 400, "WEAK_PASSWORD", e.Error())
		return
	}
	res, e := s.db.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,pushplus_token,push_token_id) VALUES(?,?,?,1,?,?)`, x.Username, hash, x.Role, x.Token, nullInt(x.TokenID))
	if e != nil {
		errorResponse(w, e)
		return
	}
	id, _ := res.LastInsertId()
	s.auditMutation(r, systemlog.CategoryAuth, "user_created", "user", id, fmt.Sprintf("账号=%s 角色=%s", x.Username, x.Role))
	writeJSON(w, 201, envelope{OK: true, Data: map[string]any{"id": id, "username": x.Username, "role": x.Role}})
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var x struct {
		Active   *bool   `json:"active"`
		Password string  `json:"password"`
		Token    *string `json:"pushplus_token"`
		TokenID  *int64  `json:"push_token_id"`
		Role     string  `json:"role"`
	}
	if e != nil || decode(r, &x) != nil {
		fail(w, 400, "BAD_REQUEST", "账号数据无效")
		return
	}
	if id == currentUser(r).ID && x.Active != nil && !*x.Active {
		fail(w, 400, "VALIDATION", "不能停用当前账号")
		return
	}
	if x.Role != "" && x.Role != "admin" && x.Role != "viewer" {
		fail(w, 400, "VALIDATION", "角色无效")
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		errorResponse(w, e)
		return
	}
	if x.Active != nil {
		_, e = tx.Exec(`UPDATE users SET active=? WHERE id=?`, boolInt(*x.Active), id)
	}
	if e == nil && x.Role != "" {
		_, e = tx.Exec(`UPDATE users SET role=? WHERE id=?`, x.Role, id)
	}
	if e == nil && x.Token != nil {
		_, e = tx.Exec(`UPDATE users SET pushplus_token=? WHERE id=?`, *x.Token, id)
	}
	if e == nil && x.TokenID != nil {
		_, e = tx.Exec(`UPDATE users SET push_token_id=? WHERE id=?`, nullInt(*x.TokenID), id)
	}
	if e == nil && x.Password != "" {
		var h string
		h, e = auth.HashPassword(x.Password)
		if e == nil {
			_, e = tx.Exec(`UPDATE users SET password_hash=?,must_change_password=1 WHERE id=?`, h, id)
		}
	}
	if e != nil {
		_ = tx.Rollback()
		errorResponse(w, e)
		return
	}
	if e = tx.Commit(); e != nil {
		errorResponse(w, e)
		return
	}
	changes := []string{}
	if x.Active != nil {
		changes = append(changes, fmt.Sprintf("启用=%t", *x.Active))
	}
	if x.Role != "" {
		changes = append(changes, "角色="+x.Role)
	}
	if x.TokenID != nil {
		changes = append(changes, fmt.Sprintf("令牌ID=%d", *x.TokenID))
	}
	if x.Password != "" {
		changes = append(changes, "密码已重置")
	}
	s.auditMutation(r, systemlog.CategoryAuth, "user_updated", "user", id, strings.Join(changes, "，"))
	ok(w, map[string]bool{"updated": true})
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	var token string
	_ = s.db.QueryRowContext(r.Context(), `SELECT value FROM settings WHERE key='pushplus_token'`).Scan(&token)
	tokens, err := push.NewStore(s.db).List()
	if err != nil {
		errorResponse(w, err)
		return
	}
	enabled := 0
	for _, t := range tokens {
		if t.Enabled {
			enabled++
		}
	}
	defaults := s.pushDefaults(r.Context())
	out := map[string]any{
		"timezone":                  s.timezone(),
		"pushplus_token_configured": token != "" || s.cfg.PushPlusToken != "",
		"pushplus_token":            "",
		"tokens_configured":         len(tokens) > 0,
		"tokens_enabled":            enabled,
		"tokens_total":              len(tokens),
	}
	for k, v := range defaults {
		out["push."+k] = v
	}
	ok(w, out)
}
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Token    string `json:"pushplus_token"`
		Timezone string `json:"timezone"`
		Channel  string `json:"push.channel"`
		Template string `json:"push.template"`
		Option   string `json:"push.option"`
		Callback string `json:"push.callback_url"`
		Timeout  int    `json:"push.timeout_seconds"`
		Retries  int    `json:"push.retry_limit"`
	}
	if decode(r, &x) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if x.Timezone != "" {
		if _, e := time.LoadLocation(x.Timezone); e != nil {
			fail(w, 400, "VALIDATION", "时区无效")
			return
		}
		if _, e := s.db.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES('timezone',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, x.Timezone); e != nil {
			errorResponse(w, e)
			return
		}
	}
	if x.Token != "" {
		if _, e := s.db.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES('pushplus_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strings.TrimSpace(x.Token)); e != nil {
			errorResponse(w, e)
			return
		}
	}
	if x.Channel != "" {
		if !push.ValidChannel(x.Channel) {
			fail(w, 400, "VALIDATION", "不支持的发送渠道："+x.Channel)
			return
		}
		s.saveSetting(r, "push.channel", x.Channel)
	}
	if x.Template != "" {
		if !push.ValidTemplate(x.Template) {
			fail(w, 400, "VALIDATION", "不支持的消息模板："+x.Template)
			return
		}
		s.saveSetting(r, "push.template", x.Template)
	}
	s.saveSetting(r, "push.option", x.Option)
	s.saveSetting(r, "push.callback_url", x.Callback)
	if x.Timeout > 0 {
		s.saveSetting(r, "push.timeout_seconds", strconv.Itoa(x.Timeout))
	}
	if x.Retries > 0 {
		s.saveSetting(r, "push.retry_limit", strconv.Itoa(x.Retries))
	}
	changes := []string{}
	if x.Timezone != "" {
		changes = append(changes, "时区="+x.Timezone)
	}
	if x.Token != "" {
		changes = append(changes, "全局 PushPlus Token 已更新")
	}
	if x.Channel != "" {
		changes = append(changes, "默认渠道="+x.Channel)
	}
	if x.Template != "" {
		changes = append(changes, "默认模板="+x.Template)
	}
	if x.Timeout > 0 {
		changes = append(changes, fmt.Sprintf("超时=%d秒", x.Timeout))
	}
	if x.Retries > 0 {
		changes = append(changes, fmt.Sprintf("重试上限=%d", x.Retries))
	}
	if x.Option != "" {
		changes = append(changes, "渠道选项已更新")
	}
	if x.Callback != "" {
		changes = append(changes, "回调地址已更新")
	}
	if len(changes) > 0 {
		s.auditMutation(r, systemlog.CategorySystem, "settings_updated", "settings", 0, strings.Join(changes, "；"))
	}
	ok(w, map[string]bool{"saved": true})
}

// saveSetting writes a single settings key.
func (s *Server) saveSetting(r *http.Request, key, value string) {
	_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
}

// pushDefaults reads the global push settings with the documented defaults.
func (s *Server) pushDefaults(ctx context.Context) map[string]string {
	out := map[string]string{"template": "html", "channel": "wechat", "timeout_seconds": "15", "retry_limit": "6"}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM settings WHERE key LIKE 'push.%'`)
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

func (s *Server) globalToken(ctx context.Context) string {
	var token string
	if s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='pushplus_token'`).Scan(&token) == nil && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}
	return strings.TrimSpace(s.cfg.PushPlusToken)
}

func (s *Server) timezone() string {
	var timezone string
	if s.db.QueryRow(`SELECT value FROM settings WHERE key='timezone'`).Scan(&timezone) == nil && strings.TrimSpace(timezone) != "" {
		return timezone
	}
	return s.cfg.Timezone
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=maintenance-export.csv")
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{"project", "customer", "contact", "vendor", "model", "serial_number", "maintenance_start", "maintenance_end", "status", "owner", "contract_no", "notes"})
	rows, e := s.db.Query(`SELECT p.name,p.customer,p.contact,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.status,d.owner,d.contract_no,d.notes FROM devices d JOIN projects p ON p.id=d.project_id ORDER BY p.name,d.serial_number`)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a [12]string
		ptr := make([]any, 12)
		for i := range a {
			ptr[i] = &a[i]
		}
		if rows.Scan(ptr...) != nil {
			return
		}
		_ = wr.Write(a[:])
	}
	wr.Flush()
}
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	f, err := os.CreateTemp("", "maintenance-backup-*.db")
	if err != nil {
		errorResponse(w, err)
		return
	}
	name := f.Name()
	_ = f.Close()
	defer os.Remove(name)
	if _, err = s.db.Exec(`VACUUM INTO ?`, name); err != nil {
		errorResponse(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", "attachment; filename=maintenance-backup.db")
	s.auditMutation(r, systemlog.CategorySystem, "database_backup_downloaded", "database", 0, "SQLite VACUUM INTO backup created and downloaded")
	http.ServeFile(w, r, name)
}

func (s *Server) auditMutation(r *http.Request, category, action, object string, id int64, detail string) {
	_ = s.log().Record(systemlog.Entry{
		Level: systemlog.LevelInfo, Category: category, Action: action,
		Message: fmt.Sprintf("%s #%d %s", object, id, action), Detail: detail,
		Actor: currentUser(r).Username, RefType: object, RefID: id, IP: clientIP(r.Context()),
	})
}

var _ = context.Background
