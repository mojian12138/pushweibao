package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/auth"
)

type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Customer    string `json:"customer"`
	Contact     string `json:"contact"`
	Notes       string `json:"notes"`
	ContractNo  string `json:"contract_no"`
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
	rows, err := s.db.Query(`SELECT p.id,p.name,p.customer,p.contact,p.notes,p.contract_no,COUNT(d.id),COALESCE(MIN(CASE WHEN d.status='active' THEN d.maintenance_end END),'') FROM projects p LEFT JOIN devices d ON d.project_id=p.id WHERE (?='' OR p.name LIKE ? OR p.customer LIKE ? OR p.contact LIKE ?) GROUP BY p.id ORDER BY p.name`, q, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Name, &p.Customer, &p.Contact, &p.Notes, &p.ContractNo, &p.DeviceCount, &p.NearestDue); err != nil {
			errorResponse(w, err)
			return
		}
		out = append(out, p)
	}
	ok(w, out)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var p Project
	if decode(r, &p) != nil || strings.TrimSpace(p.Name) == "" {
		fail(w, 400, "VALIDATION", "项目名必填")
		return
	}
	res, err := s.db.Exec(`INSERT INTO projects(name,customer,contact,notes,contract_no) VALUES(?,?,?,?,?)`, strings.TrimSpace(p.Name), p.Customer, p.Contact, p.Notes, p.ContractNo)
	if err != nil {
		errorResponse(w, err)
		return
	}
	p.ID, _ = res.LastInsertId()
	writeJSON(w, 201, envelope{OK: true, Data: p})
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var p Project
	if e != nil || decode(r, &p) != nil || strings.TrimSpace(p.Name) == "" {
		fail(w, 400, "VALIDATION", "项目数据无效")
		return
	}
	res, err := s.db.Exec(`UPDATE projects SET name=?,customer=?,contact=?,notes=?,contract_no=? WHERE id=?`, p.Name, p.Customer, p.Contact, p.Notes, p.ContractNo, id)
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
	ok(w, p)
}
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	deleteByID(w, r, s.db, "projects", "项目")
}
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	project := r.URL.Query().Get("project_id")
	status := r.URL.Query().Get("status")
	bucket := r.URL.Query().Get("bucket")
	vendor := r.URL.Query().Get("vendor")
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	today := time.Now().In(mustLocation(s.cfg.Timezone)).Format("2006-01-02")
	query := `SELECT d.id,d.project_id,p.name,p.customer,d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.status,d.notes,d.owner,d.contract_no,d.strategy_override,CAST(julianday(d.maintenance_end)-julianday(?) AS INTEGER) FROM devices d JOIN projects p ON p.id=d.project_id WHERE (?='' OR d.project_id=?) AND (?='' OR d.status=?) AND (?='' OR d.vendor LIKE ?) AND (?='' OR d.maintenance_end>=?) AND (?='' OR d.maintenance_end<=?) AND (?='' OR (d.maintenance_end<? AND d.status='active') OR (?='soon30' AND d.maintenance_end>=? AND d.maintenance_end<=date(?,'+30 day')) OR (?='soon90' AND d.maintenance_end>=? AND d.maintenance_end<=date(?,'+90 day')) OR (?='active' AND d.status='active')) AND (?='' OR d.vendor LIKE ? OR d.model LIKE ? OR d.serial_number LIKE ? OR p.name LIKE ? OR p.customer LIKE ?) ORDER BY d.maintenance_end,d.id`
	rows, err := s.db.Query(query, today, project, project, status, status, vendor, "%"+vendor+"%", from, from, to, to, bucket, today, bucket, bucket, today, today, bucket, bucket, today, today, q, "%"+q+"%", "%"+q+"%", "%"+q+"%", "%"+q+"%", "%"+q+"%")
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
	writeJSON(w, 201, envelope{OK: true, Data: d})
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
	ok(w, d)
}
func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	deleteByID(w, r, s.db, "devices", "设备")
}
func deleteByID(w http.ResponseWriter, r *http.Request, database *sql.DB, table, label string) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	res, err := database.Exec(`DELETE FROM `+table+` WHERE id=?`, id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", label+"不存在")
		return
	}
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
	today := time.Now().In(mustLocation(s.cfg.Timezone)).Format("2006-01-02")
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
		_ = rows.Scan(&n, &c)
		v.Vendors = append(v.Vendors, map[string]any{"name": n, "count": c})
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
		_ = rows.Scan(&m, &c)
		v.Monthly = append(v.Monthly, map[string]any{"month": m, "count": c})
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
	Recipients     []string `json:"recipients"`
}

func (s *Server) listStrategies(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,name,type,enabled,sort_order,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,recipients FROM strategies ORDER BY sort_order,id`)
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
		if e = rows.Scan(&x.ID, &x.Name, &x.Type, &enabled, &x.Sort, &offsets, &x.MonthlyDay, &x.Horizon, &x.CycleThreshold, &x.CycleInterval, &x.Template, &recipients); e != nil {
			errorResponse(w, e)
			return
		}
		x.Enabled = enabled == 1
		_ = json.Unmarshal([]byte(offsets), &x.Offsets)
		_ = json.Unmarshal([]byte(recipients), &x.Recipients)
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
	off, _ := json.Marshal(x.Offsets)
	rec, _ := json.Marshal(x.Recipients)
	if create {
		res, e := s.db.Exec(`INSERT INTO strategies(name,type,enabled,sort_order,offsets,monthly_day,horizon_days,cycle_threshold,cycle_interval,template,recipients) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, x.Name, x.Type, boolInt(x.Enabled), x.Sort, string(off), x.MonthlyDay, x.Horizon, x.CycleThreshold, x.CycleInterval, x.Template, string(rec))
		if e != nil {
			errorResponse(w, e)
			return
		}
		x.ID, _ = res.LastInsertId()
	} else {
		res, e := s.db.Exec(`UPDATE strategies SET name=?,type=?,enabled=?,sort_order=?,offsets=?,monthly_day=?,horizon_days=?,cycle_threshold=?,cycle_interval=?,template=?,recipients=? WHERE id=?`, x.Name, x.Type, boolInt(x.Enabled), x.Sort, string(off), x.MonthlyDay, x.Horizon, x.CycleThreshold, x.CycleInterval, x.Template, string(rec), id)
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
	}
	ok(w, x)
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
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
	deleteByID(w, r, s.db, "strategies", "策略")
}
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,project_name,strategy_name,trigger_point,channel,recipient,message,status,last_error,created_at,sent_at FROM notification_outbox ORDER BY id DESC LIMIT 500`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	cols := []string{"id", "project", "strategy", "trigger", "channel", "recipient", "message", "status", "error", "created_at", "sent_at"}
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
	res, e := s.db.Exec(`UPDATE notification_outbox SET status='pending',next_attempt=CURRENT_TIMESTAMP,last_error='' WHERE id=? AND status!='sent'`, id)
	if e != nil {
		errorResponse(w, e)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "可重推记录不存在")
		return
	}
	ok(w, map[string]bool{"queued": true})
}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,username,role,active,must_change_password,created_at FROM users ORDER BY id`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, role, created string
		var active, must int
		if e = rows.Scan(&id, &name, &role, &active, &must, &created); e != nil {
			errorResponse(w, e)
			return
		}
		out = append(out, map[string]any{"id": id, "username": name, "role": role, "active": active == 1, "must_change_password": must == 1, "created_at": created})
	}
	ok(w, out)
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Token    string `json:"pushplus_token"`
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
	res, e := s.db.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,pushplus_token) VALUES(?,?,?,1,?)`, x.Username, hash, x.Role, x.Token)
	if e != nil {
		errorResponse(w, e)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, envelope{OK: true, Data: map[string]any{"id": id, "username": x.Username, "role": x.Role}})
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var x struct {
		Active   *bool   `json:"active"`
		Password string  `json:"password"`
		Token    *string `json:"pushplus_token"`
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
	ok(w, map[string]bool{"updated": true})
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	var token string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='pushplus_token'`).Scan(&token)
	ok(w, map[string]any{"timezone": s.cfg.Timezone, "pushplus_token_configured": token != "" || s.cfg.PushPlusToken != "", "pushplus_token": ""})
}
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Token    string `json:"pushplus_token"`
		Timezone string `json:"timezone"`
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
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('timezone',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, x.Timezone)
	}
	if x.Token != "" {
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('pushplus_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, x.Token)
	}
	ok(w, map[string]bool{"saved": true})
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
	http.ServeFile(w, r, name)
}

var _ = context.Background
