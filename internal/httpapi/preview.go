package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/template"
)

// previewRequest asks for a rendered preview of a template.
//
// The kind decides which placeholders are available. The mode decides which
// data fills them: "real" uses a record from the database, "sample" uses the
// built-in sample values.
type previewRequest struct {
	Kind    string `json:"kind"`
	Mode    string `json:"mode"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Item    string `json:"item"`
	// Format is the way the message is written: html, txt or markdown.
	Format string `json:"format"`
	// DeviceID selects the record used when the mode is "real".
	DeviceID int64 `json:"device_id"`
}

// previewResponse is the rendered preview.
type previewResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	// Placeholders lists the markers of the template, so that the editor can
	// show them and insert them.
	Placeholders []template.Placeholder `json:"placeholders"`
	// TitlePlaceholders lists the markers a title accepts.
	TitlePlaceholders []template.Placeholder `json:"title_placeholders"`
	// Unknown lists the markers that the kind does not support.
	Unknown []string `json:"unknown"`
	// Item is the rendered layout of one summary line, when a kind has one.
	Item string `json:"item"`
	// UsedDeviceID reports the record the preview was rendered from.
	UsedDeviceID int64 `json:"used_device_id"`
	// Mode reports the mode that was actually used.
	Mode string `json:"mode"`
	// Format reports the way the message is written, so that the editor can
	// show the preview the way the phone will show it.
	Format string `json:"format"`
	// HTML is the message written as html, so that the preview shows the
	// layout of the phone instead of the source of the message.
	HTML string `json:"html"`
}

// previewStrategy renders a strategy template with real or sample data.
func (s *Server) previewStrategy(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = template.KindMilestone
	}
	if kind != template.KindMilestone && kind != template.KindMonthly && kind != template.KindCycle {
		fail(w, 400, "VALIDATION", "策略类型无效")
		return
	}

	// A template that uses a marker the kind does not support is reported as
	// an error, the same way the save does.
	if e := template.Validate(kind, req.Content, false); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	if e := template.ValidateTitle(req.Title); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}

	out := previewResponse{
		Placeholders:      template.PlaceholdersFor(kind, false),
		TitlePlaceholders: template.PlaceholdersFor(kind, true),
		Unknown:           template.Unknown(kind, req.Content, false),
		Mode:              req.Mode,
		Format:            template.NormalizeFormat(req.Format),
	}
	if out.Mode == "" {
		out.Mode = "real"
	}

	if out.Mode == "sample" {
		out.Content = template.Sample(kind, req.Content)
		out.Title = template.SampleTitle(req.Title)
		if req.Item != "" {
			out.Item = template.SampleItem(req.Item)
		}
		respondPreview(w, out)
		return
	}

	// The real mode renders with a record from the database.
	if kind == template.KindMonthly {
		summary, err := s.buildPreviewSummary(r, req.Item)
		if err != nil {
			fail(w, 500, "INTERNAL", err.Error())
			return
		}
		out.Content = template.RenderSummary(req.Content, summary)
		out.Title = template.RenderTitleSummary(req.Title, summary)
		if req.Item != "" {
			out.Item = template.RenderItem(req.Item, summary.Device, summary.Days)
		}
		respondPreview(w, out)
		return
	}

	d, days, id, err := s.loadPreviewDevice(r, req.DeviceID)
	if err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	out.UsedDeviceID = id
	out.Content = template.Render(req.Content, d, days)
	out.Title = template.RenderTitle(req.Title, d, days)
	if req.Item != "" {
		out.Item = template.RenderItem(req.Item, d, days)
	}
	respondPreview(w, out)
}

// respondPreview sends a rendered preview.
//
// The message is also written as html, so that the editor can show the
// layout the phone will show instead of the source of the message.
func respondPreview(w http.ResponseWriter, out previewResponse) {
	out.HTML = template.RenderAsHTML(out.Format, out.Content)
	ok(w, out)
}

// previewDevice renders a template with the data of one device, so that the
// test push of a device shows what the real message looks like.
func (s *Server) previewDevice(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var req previewRequest
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	d, days, _, err := s.loadPreviewDevice(r, id)
	if err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	// A template that uses a marker the kind does not support is reported as
	// an error, the same way the strategy preview does. Otherwise the marker
	// would stay in the text and reach the phone as it is.
	if e := template.Validate(template.KindMilestone, req.Content, false); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	if e := template.ValidateTitle(req.Title); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	out := previewResponse{
		Placeholders:      template.PlaceholdersFor(template.KindMilestone, false),
		TitlePlaceholders: template.PlaceholdersFor(template.KindMilestone, true),
		Mode:              "real",
		UsedDeviceID:      id,
		Format:            template.NormalizeFormat(req.Format),
	}
	if req.Title != "" {
		out.Title = template.RenderTitle(req.Title, d, days)
	}
	out.Content = template.Render(req.Content, d, days)
	if req.Item != "" {
		out.Item = template.RenderItem(req.Item, d, days)
	}
	respondPreview(w, out)
}

// previewToken renders the default test message of a token.
func (s *Server) previewToken(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	// A template that uses a marker the kind does not support is reported as
	// an error, the same way the strategy preview does. Otherwise the marker
	// would stay in the text and reach the phone as it is.
	if e := template.Validate(template.KindMilestone, req.Content, false); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	if e := template.ValidateTitle(req.Title); e != nil {
		fail(w, 400, "VALIDATION", e.Error())
		return
	}
	out := previewResponse{
		Placeholders:      template.PlaceholdersFor(template.KindMilestone, false),
		TitlePlaceholders: template.PlaceholdersFor(template.KindMilestone, true),
		Mode:              req.Mode,
		Format:            template.NormalizeFormat(req.Format),
	}
	if out.Mode == "" {
		out.Mode = "sample"
	}
	if out.Mode == "sample" {
		out.Content = template.Sample(template.KindMilestone, req.Content)
		out.Title = template.SampleTitle(req.Title)
		respondPreview(w, out)
		return
	}
	d, days, id, err := s.loadPreviewDevice(r, req.DeviceID)
	if err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	out.UsedDeviceID = id
	out.Content = template.Render(req.Content, d, days)
	out.Title = template.RenderTitle(req.Title, d, days)
	respondPreview(w, out)
}

// loadPreviewDevice reads a device with its project. When no id is given the
// device that is closest to expiring is used, so that the preview shows a
// record that matters.
func (s *Server) loadPreviewDevice(r *http.Request, id int64) (template.Device, int, int64, error) {
	d := template.Device{}
	var end string
	query := `SELECT d.id,p.name,p.customer,COALESCE(p.contact,''),d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner)
		FROM devices d JOIN projects p ON p.id=d.project_id`
	var args []any
	if id > 0 {
		query += ` WHERE d.id=?`
		args = append(args, id)
	} else {
		query += ` WHERE d.status='active' ORDER BY d.maintenance_end LIMIT 1`
	}
	row := s.db.QueryRow(query, args...)
	e := row.Scan(&id, &d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &d.Start, &end, &d.Notes, &d.Owner)
	if e != nil {
		return d, 0, 0, fmt.Errorf("没有可用于预览的设备")
	}
	d.End = end
	days := s.daysUntil(end)
	return d, days, id, nil
}

// buildPreviewSummary builds the summary of the monthly preview from the
// devices that are close to expiring. The item template is the one of the
// strategy, so that the preview shows the real layout.
func (s *Server) buildPreviewSummary(r *http.Request, itemTemplate string) (template.Summary, error) {
	if itemTemplate == "" {
		itemTemplate = DefaultItemTemplate
	}
	out := template.Summary{Month: s.currentMonth()}
	rows, e := s.db.Query(`SELECT p.name,p.customer,COALESCE(p.contact,''),d.vendor,d.model,d.serial_number,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner)
		FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.status='active' ORDER BY d.maintenance_end LIMIT 50`)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	var upcoming, expired strings.Builder
	for rows.Next() {
		var d template.Device
		var end string
		if e = rows.Scan(&d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &end, &d.Notes, &d.Owner); e != nil {
			return out, e
		}
		d.End = end
		days := s.daysUntil(end)
		if out.Device.Project == "" {
			out.Device = d
			out.Days = days
		}
		line := template.RenderItem(itemTemplate, d, days)
		if days < 0 {
			expired.WriteString(line)
			out.ExpiredCount++
		} else {
			upcoming.WriteString(line)
			out.Count++
		}
	}
	out.Items = upcoming.String()
	out.Expired = expired.String()
	return out, nil
}

// daysUntil returns the number of days between today and the end date. A
// date that cannot be read counts as expiring today.
func (s *Server) daysUntil(end string) int {
	today := s.today()
	a, e1 := time.Parse("2006-01-02", end)
	b, e2 := time.Parse("2006-01-02", today)
	if e1 != nil || e2 != nil {
		return 0
	}
	return int(a.Sub(b).Hours() / 24)
}

// today returns the current date in the configured timezone.
func (s *Server) today() string {
	return time.Now().In(mustLocation(s.cfg.Timezone)).Format("2006-01-02")
}

// currentMonth returns the current month in the configured timezone.
func (s *Server) currentMonth() string {
	return time.Now().In(mustLocation(s.cfg.Timezone)).Format("2006-01")
}

// DefaultItemTemplate is the layout of one line of the monthly summary.
const DefaultItemTemplate = "项目：{{project}} 客户：{{customer}} 型号：{{model}} SN：{{sn}} 到期：{{end}} 剩余：{{days}} 负责人：{{owner}}\n"

// markerKeys returns the markers a text still holds, so that a message with
// an unresolved marker can be refused.
func markerKeys(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range template.MarkerPattern().FindAllStringSubmatch(text, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// unresolvedMarkers returns the markers of a message that the renderer could
// not fill, so that the caller can refuse to send it.
//
// Both the title and the body are checked: a message is only worth sending
// when it reads as a finished text.
func unresolvedMarkers(title, content string) []string {
	out := markerKeys(content)
	seen := map[string]bool{}
	for _, k := range out {
		seen[k] = true
	}
	for _, k := range markerKeys(title) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
