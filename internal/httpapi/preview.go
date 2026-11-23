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
	// Horizon limits the summary of a monthly preview to the devices that
	// run out inside so many days, the same way the monthly scan does.
	Horizon int `json:"horizon_days"`
	// Window is the width of the window of a milestone preview, in days.
	// The tiers are drawn from the offsets of the strategy in the editor,
	// so the preview carries them in this list.
	Window int `json:"window_days"`
	// Offsets are the tiers of the milestone preview, the ones the strategy
	// fires on. An empty list falls back to the default tiers.
	Offsets []int `json:"offsets"`
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
	// WindowCount reports the number of devices the window of a milestone
	// preview holds, so the editor can show what the message covers.
	WindowCount int `json:"window_count,omitempty"`
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
		// The sample fills every marker the renderer knows, so a template
		// written for another kind still shows a filled message instead of
		// its raw markers.
		out.Title = template.SampleTitle(req.Title)
		out.Content = template.RenderAny(req.Content, template.SampleDevice(), 30,
			template.SampleMilestoneItems(out.Format),
			sampleSummary(out.Format, req.Item, s.currentMonth()))
		if req.Item != "" {
			out.Item = template.SampleItem(req.Item)
		}
		respondPreview(w, out)
		return
	}

	// The real mode renders with the records of the database. What the
	// template asks for decides which records: a body that asks for a
	// summary shows the devices of the horizon, a body that asks for a list
	// shows the devices of the window, and the rest shows one device.
	if template.AsksForSummary(req.Content) {
		// The summary covers the horizon of the strategy, so the preview
		// filters the devices the same way the monthly scan does. A
		// preview request that does not name a horizon uses 90 days, the
		// default of the strategies.
		horizon := req.Horizon
		if horizon <= 0 {
			horizon = 90
		}
		summary, rows, err := s.buildPreviewSummary(r, req.Item, out.Format, horizon)
		if err != nil {
			fail(w, 500, "INTERNAL", err.Error())
			return
		}
		// A summary template may also hold the grouped list, so the list is
		// laid out from the same rows instead of being left as a marker.
		hits := make([]template.MilestoneHit, 0, len(rows))
		for _, row := range rows {
			hits = append(hits, template.MilestoneHit{Device: row.Device, Days: row.Days})
		}
		items := template.BuildMilestoneItems(out.Format, template.DefaultOffsets, hits)
		out.Content = template.RenderAny(req.Content, summary.Device, summary.Days, items, summary)
		out.Title = template.RenderTitleSummary(req.Title, summary)
		out.WindowCount = summary.Count + summary.ExpiredCount
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
	// The grouped list of a milestone message is filled with the devices
	// of the window, grouped and sorted the way the scan lays them out, so
	// the preview shows the message the strategy will really send. A
	// request that carries a device id previews that one device, as the
	// test dialog of a device does.
	if template.KnownMarker(req.Content, "milestone_items") {
		items, repDevice, repDays, count, rows := s.buildMilestoneWindowFor(r, req, out.Format)
		out.Content = template.RenderAny(req.Content, repDevice, repDays, items, s.summaryOfRows(req.Item, out.Format, rows, repDevice, repDays))
		out.Title = template.RenderTitleSummary(req.Title, s.summaryOfRows(req.Item, out.Format, rows, repDevice, repDays))
		out.WindowCount = count
	} else {
		summary := s.summaryOfRows(req.Item, out.Format, []template.SummaryRow{{Device: d, Days: days}}, d, days)
		out.Content = template.RenderAny(req.Content, d, days, template.MilestoneItemsForOne(out.Format, d, days), summary)
		out.Title = template.RenderTitleSummary(req.Title, summary)
	}
	if req.Item != "" {
		out.Item = template.RenderItem(req.Item, d, days)
	}
	respondPreview(w, out)
}

// summaryOfRows lays out the summary of the rows a preview covers, so that a
// template that asks for a summary is filled from the same devices the list
// shows.
func (s *Server) summaryOfRows(itemTemplate, format string, rows []template.SummaryRow, device template.Device, days int) template.Summary {
	if strings.TrimSpace(itemTemplate) == "" {
		itemTemplate = template.DefaultItemFor(format)
	}
	upcomingSection, expiredSection := template.BuildSummaryBlocks(format, itemTemplate, rows)
	upcomingLines, expiredLines := template.BuildSummaryLines(itemTemplate, rows)
	summary := template.Summary{
		Month:           s.currentMonth(),
		Items:           upcomingLines,
		Expired:         expiredLines,
		UpcomingSection: upcomingSection,
		ExpiredSection:  expiredSection,
		Device:          device,
		Days:            days,
	}
	for _, row := range rows {
		if row.Days < 0 {
			summary.ExpiredCount++
		} else {
			summary.Count++
		}
	}
	return summary
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
	// Every marker is filled, so a template written for another kind never
	// reaches the phone as raw text through the test of a single device.
	summary := s.summaryOfRows(req.Item, out.Format, []template.SummaryRow{{Device: d, Days: days}}, d, days)
	out.Content = template.RenderAny(req.Content, d, days, template.MilestoneItemsForOne(out.Format, d, days), summary)
	if req.Title != "" {
		out.Title = template.RenderTitleSummary(req.Title, summary)
	}
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
		out.Title = template.SampleTitle(req.Title)
		out.Content = template.RenderAny(req.Content, template.SampleDevice(), 30,
			template.SampleMilestoneItems(out.Format),
			sampleSummary(out.Format, req.Item, s.currentMonth()))
		respondPreview(w, out)
		return
	}
	d, days, id, err := s.loadPreviewDevice(r, req.DeviceID)
	if err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	out.UsedDeviceID = id
	summary := s.summaryOfRows(req.Item, out.Format, []template.SummaryRow{{Device: d, Days: days}}, d, days)
	out.Content = template.RenderAny(req.Content, d, days, template.MilestoneItemsForOne(out.Format, d, days), summary)
	if req.Title != "" {
		out.Title = template.RenderTitleSummary(req.Title, summary)
	}
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

// buildMilestoneWindow lays the devices of the window out the way the scan
// does: every device that still runs inside the widest tier goes into the
// list, grouped and sorted by urgency. A request that names a device
// previews that device alone. The representative device is the most urgent
// one of the window, so the markers of a single device name it.
func (s *Server) buildMilestoneWindow(format string, deviceID int64, windowDays int, offsets []int) (string, template.Device, int, int, []template.SummaryRow) {
	if len(offsets) == 0 {
		offsets = template.DefaultOffsets
	}
	widest := windowDays
	if widest <= 0 {
		for _, o := range offsets {
			if o > widest {
				widest = o
			}
		}
	}
	rows, e := s.db.Query(`SELECT d.id,p.name,p.customer,COALESCE(p.contact,''),d.vendor,d.model,d.serial_number,d.maintenance_start,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner)
		FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.status='active' ORDER BY d.maintenance_end LIMIT 500`)
	if e != nil {
		return "", template.Device{}, 0, 0, nil
	}
	defer rows.Close()
	hits := []template.MilestoneHit{}
	windowRows := []template.SummaryRow{}
	for rows.Next() {
		var d template.Device
		var id int64
		var end string
		if e = rows.Scan(&id, &d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &d.Start, &end, &d.Notes, &d.Owner); e != nil {
			continue
		}
		d.End = end
		if deviceID > 0 && id != deviceID {
			continue
		}
		days := s.daysUntil(end)
		if days < 0 || days > widest {
			continue
		}
		hits = append(hits, template.MilestoneHit{Device: d, ID: id, Days: days})
		windowRows = append(windowRows, template.SummaryRow{Device: d, Days: days})
	}
	if len(hits) == 0 {
		// A window without devices falls back to the closest device, so
		// the preview still shows the layout of the message.
		if d, days, _, err := s.loadPreviewDevice(rDummy(), 0); err == nil {
			one := []template.SummaryRow{{Device: d, Days: days}}
			return template.BuildMilestoneItems(format, offsets, []template.MilestoneHit{{Device: d, Days: days}}), d, days, 0, one
		}
		return "", template.Device{}, 0, 0, nil
	}
	rep := hits[0]
	for _, h := range hits[1:] {
		if h.Days < rep.Days {
			rep = h
		}
	}
	return template.BuildMilestoneItems(format, offsets, hits), rep.Device, rep.Days, len(hits), windowRows
}

// rDummy is the request the fallback of the window builder hands on, so
// that the loader can pick the closest device on its own.
func rDummy() *http.Request { return nil }

// buildMilestoneWindowFor lays the devices of the window out for a preview
// request, with the tiers the request carries.
func (s *Server) buildMilestoneWindowFor(r *http.Request, req previewRequest, format string) (string, template.Device, int, int, []template.SummaryRow) {
	return s.buildMilestoneWindow(format, req.DeviceID, req.Window, req.Offsets)
}
func (s *Server) buildPreviewSummary(r *http.Request, itemTemplate, format string, horizon int) (template.Summary, []template.SummaryRow, error) {
	if itemTemplate == "" {
		itemTemplate = DefaultItemTemplate
	}
	out := template.Summary{Month: s.currentMonth()}
	rows, e := s.db.Query(`SELECT p.name,p.customer,COALESCE(p.contact,''),d.vendor,d.model,d.serial_number,d.maintenance_end,d.notes,COALESCE(NULLIF(d.owner,''),p.owner)
		FROM devices d JOIN projects p ON p.id=d.project_id
		WHERE d.status='active' AND (d.maintenance_end<date(?) OR d.maintenance_end<=date(?, '+' || ? || ' day'))
		ORDER BY d.maintenance_end LIMIT 50`, s.today(), s.today(), horizon)
	if e != nil {
		return out, nil, e
	}
	defer rows.Close()
	var summaryRows []template.SummaryRow
	for rows.Next() {
		var d template.Device
		var end string
		if e = rows.Scan(&d.Project, &d.Customer, &d.Contact, &d.Vendor, &d.Model, &d.SN, &end, &d.Notes, &d.Owner); e != nil {
			return out, nil, e
		}
		d.End = end
		days := s.daysUntil(end)
		if out.Device.Project == "" {
			out.Device = d
			out.Days = days
		}
		summaryRows = append(summaryRows, template.SummaryRow{Device: d, Days: days})
	}
	// The lines of the {{items}} and {{expired}} markers and the blocks of
	// the section markers are laid out by the same builder, so both show
	// the devices in the order of their urgency.
	out.Items, out.Expired = template.BuildSummaryLines(itemTemplate, summaryRows)
	out.UpcomingSection, out.ExpiredSection = template.BuildSummaryBlocks(format, itemTemplate, summaryRows)
	for _, r := range summaryRows {
		if r.Days < 0 {
			out.ExpiredCount++
		} else {
			out.Count++
		}
	}
	return out, summaryRows, nil
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
