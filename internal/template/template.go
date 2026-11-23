// Package template renders the message bodies used for maintenance reminders.
//
// A template is plain text with {{placeholder}} markers. Which markers are
// available depends on the kind of reminder, so every reminder type has its
// own set. The package also validates templates: a marker that the reminder
// type does not provide is rejected instead of being silently replaced with
// an empty string, because a message that goes out half empty is worse than
// a save that asks for one more click.
package template

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Strategy kinds. These match the values stored in the strategies table.
const (
	KindMilestone = "milestone"
	KindMonthly   = "monthly"
	KindCycle     = "cycle"
)

// Placeholder describes one marker that a template may use.
type Placeholder struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Sample is the value shown when the template is previewed with sample
	// data instead of a real record.
	Sample string `json:"sample"`
}

// markerPattern finds the {{name}} markers in a template.
var markerPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// MarkerPattern returns the pattern that finds a marker in a template. It
// lets the server check a message for markers that are still unresolved.
func MarkerPattern() *regexp.Regexp { return markerPattern }

// deviceMarkers are available to the reminders that are built around a
// single device.
var deviceMarkers = []Placeholder{
	{Key: "project", Label: "项目名称", Sample: "示例项目"},
	{Key: "customer", Label: "客户名称", Sample: "示例客户"},
	{Key: "contact", Label: "客户联系人", Sample: "张三"},
	{Key: "vendor", Label: "设备厂商", Sample: "示例厂商"},
	{Key: "model", Label: "设备型号", Sample: "XC-200"},
	{Key: "sn", Label: "设备序列号", Sample: "SN000123"},
	{Key: "start", Label: "维保开始日期", Sample: "2026-01-01"},
	{Key: "end", Label: "维保到期日期", Sample: "2026-12-31"},
	{Key: "days", Label: "剩余天数", Sample: "30"},
	{Key: "notes", Label: "设备备注", Sample: "备注信息"},
	{Key: "owner", Label: "负责人", Sample: "李四"},
}

// milestoneMarkers are available to the milestone reminder. They cover a
// single device and the grouped list of the devices of the window, so that
// a template can hold either one.
var milestoneMarkers = append(append([]Placeholder{}, deviceMarkers...),
	Placeholder{Key: "milestone_items", Label: "窗口内设备列表（按档位分组）", Sample: "### ⏰ 剩余 ≤30 天 · 2 台\n\n| 项目 | 设备 | SN | 到期 | 剩余 | 负责人 |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n| 示例项目 | 示例厂商 XC-200 | SN000123 | 2026-12-31 | **30 天** | 李四 |\n| 示例项目 | 示例厂商 XC-100 | SN000456 | 2026-12-29 | **28 天** | 李四 |"},
)

// summaryMarkers are available to the monthly summary, which covers many
// devices at once.
var summaryMarkers = []Placeholder{
	{Key: "month", Label: "汇总月份", Sample: "2026-11"},
	{Key: "count", Label: "即将到期设备数", Sample: "3"},
	{Key: "items", Label: "即将到期设备明细", Sample: "项目：示例项目 客户：示例客户 型号：XC-200 SN：SN000123 到期：2026-12-31 剩余：30 负责人：李四"},
	{Key: "expired", Label: "已过期设备明细", Sample: "项目：示例项目 客户：示例客户 型号：XC-100 SN：SN000456 到期：2026-10-01 剩余：-5 负责人：王五"},
	{Key: "expired_count", Label: "已过期设备数", Sample: "1"},
	{Key: "upcoming_section", Label: "即将到期区块（带小标题与统计）", Sample: "### 🔔 即将到期 · 2 台\n\n| 项目 | 型号 | SN | 到期 | 剩余 | 负责人 |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n| 示例项目 | XC-200 | SN000123 | 2026-12-31 | **30 天** | 李四 |\n| 示例项目 | XC-100 | SN000456 | 2026-12-08 | **7 天** | 李四 |"},
	{Key: "expired_section", Label: "已过期区块（带小标题与统计）", Sample: "### ⛔ 已过期 · 1 台\n\n| 项目 | 型号 | SN | 到期 | 剩余 | 负责人 |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n| 示例项目 | XC-100 | SN000456 | 2026-10-01 | **-5 天** | 王五 |"},
}

// itemMarkers are available in the template that lays out one line of the
// monthly summary.
var itemMarkers = []Placeholder{
	{Key: "project", Label: "项目名称", Sample: "示例项目"},
	{Key: "customer", Label: "客户名称", Sample: "示例客户"},
	{Key: "vendor", Label: "设备厂商", Sample: "示例厂商"},
	{Key: "model", Label: "设备型号", Sample: "XC-200"},
	{Key: "sn", Label: "设备序列号", Sample: "SN000123"},
	{Key: "end", Label: "维保到期日期", Sample: "2026-12-31"},
	{Key: "days", Label: "剩余天数", Sample: "30"},
	{Key: "owner", Label: "负责人", Sample: "李四"},
}

// ItemPlaceholders returns the markers a monthly item template accepts.
func ItemPlaceholders() []Placeholder { return itemMarkers }

// titleMarkers can be used in a title template as well as in a body.
var titleMarkers = []Placeholder{
	{Key: "project", Label: "项目名称", Sample: "示例项目"},
	{Key: "customer", Label: "客户名称", Sample: "示例客户"},
	{Key: "device", Label: "设备厂商与型号", Sample: "示例厂商 XC-200"},
	{Key: "sn", Label: "设备序列号", Sample: "SN000123"},
	{Key: "end", Label: "维保到期日期", Sample: "2026-12-31"},
	{Key: "days", Label: "剩余天数", Sample: "30"},
	{Key: "owner", Label: "负责人", Sample: "李四"},
	{Key: "month", Label: "汇总月份", Sample: "2026-11"},
	{Key: "count", Label: "即将到期设备数", Sample: "3"},
}

// Device is the record a reminder is rendered from. It mirrors the columns
// of the devices table joined with its project.
type Device struct {
	Project  string
	Customer string
	Contact  string
	Vendor   string
	Model    string
	SN       string
	Start    string
	End      string
	Notes    string
	Owner    string
}

// Summary is the data a monthly reminder is rendered from.
type Summary struct {
	Month        string
	Count        int
	Items        string
	Expired      string
	ExpiredCount int
	// UpcomingSection and ExpiredSection hold the blocks the summary
	// builder laid out: a heading with the count and, under it, the lines
	// of the devices. They fill {{upcoming_section}} and
	// {{expired_section}} of the template.
	UpcomingSection string
	ExpiredSection  string
	// Device and Days hold one representative record, so that the preview of
	// a monthly reminder can also show a rendered item line.
	Device Device
	Days   int
}

// PlaceholdersFor returns the markers a reminder kind accepts. The title has
// its own, smaller set because a title is a single short line.
func PlaceholdersFor(kind string, forTitle bool) []Placeholder {
	if forTitle {
		return titleMarkers
	}
	switch kind {
	case KindMonthly:
		return summaryMarkers
	case KindMilestone:
		return milestoneMarkers
	default:
		return deviceMarkers
	}
}

// KnownFor reports whether a marker is available for a reminder kind.
func KnownFor(kind, key string, forTitle bool) bool {
	for _, p := range PlaceholdersFor(kind, forTitle) {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Unknown lists the markers in a template that the kind does not support.
func Unknown(kind, text string, forTitle bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !KnownFor(kind, key, forTitle) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// AllMarkers lists every marker the renderer can fill, whatever kind a
// template was written for. A strategy may send any template, so a marker
// of another kind is not an error as long as the renderer knows it.
func AllMarkers() []Placeholder {
	out := []Placeholder{}
	seen := map[string]bool{}
	for _, set := range [][]Placeholder{deviceMarkers, milestoneMarkers, summaryMarkers, titleMarkers} {
		for _, p := range set {
			if seen[p.Key] {
				continue
			}
			seen[p.Key] = true
			out = append(out, p)
		}
	}
	return out
}

// KnownAnywhere reports whether the renderer can fill a marker at all.
func KnownAnywhere(key string) bool {
	for _, p := range AllMarkers() {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Validate checks a template and returns a readable message when it cannot
// be used. An empty template is accepted: the caller falls back to a default.
//
// A strategy may point at a template written for another kind, so a marker
// of another kind is accepted as long as the renderer can fill it; only a
// marker nothing knows is refused, because that one would reach the phone
// as a raw "{{name}}".
func Validate(kind, text string, forTitle bool) error {
	unknown := []string{}
	for _, key := range Unknown(kind, text, forTitle) {
		if !KnownAnywhere(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	available := []string{}
	for _, p := range PlaceholdersFor(kind, forTitle) {
		available = append(available, "{{"+p.Key+"}}")
	}
	return fmt.Errorf("模板包含不支持的占位符：%s。该类型可用：%s",
		strings.Join(unknown, "、"), strings.Join(available, "、"))
}

// Render replaces the markers in a template with the values of a device.
func Render(text string, d Device, days int) string {
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
	}
	return replace(text, values)
}

// RenderMilestone replaces the markers of a milestone template. The device
// markers are filled from the representative record, the one the message
// is about; {{milestone_items}} is filled with the grouped list of every
// device that fired, or with the single device when the message holds one.
func RenderMilestone(text, items string, d Device, days int) string {
	values := map[string]string{
		"project":         d.Project,
		"customer":        d.Customer,
		"contact":         d.Contact,
		"vendor":          d.Vendor,
		"model":           d.Model,
		"sn":              d.SN,
		"start":           d.Start,
		"end":             d.End,
		"days":            fmt.Sprint(days),
		"notes":           d.Notes,
		"owner":           d.Owner,
		"device":          strings.TrimSpace(d.Vendor + " " + d.Model),
		"milestone_items": items,
	}
	return replace(text, values)
}

// RenderAny renders a template whatever kind it was written for. A
// strategy may point at any template, so the markers of a single device,
// of the grouped window list and of the summary are all filled from the
// same data: whatever the template asks for comes out filled instead of
// reaching the phone as a raw marker.
//
// A marker the message has no data for is filled with an empty text, so a
// summary marker inside a single-device message leaves a blank spot rather
// than "{{items}}".
func RenderAny(text string, d Device, days int, items string, s Summary) string {
	values := map[string]string{
		// The markers of a single device.
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
		// The grouped list of a window message.
		"milestone_items": items,
		// The markers of a summary.
		"month":            s.Month,
		"count":            fmt.Sprint(s.Count),
		"items":            s.Items,
		"expired":          s.Expired,
		"expired_count":    fmt.Sprint(s.ExpiredCount),
		"upcoming_section": s.UpcomingSection,
		"expired_section":  s.ExpiredSection,
	}
	return replace(text, values)
}

// RenderSummary replaces the markers in a monthly template. The section
// markers {{upcoming_section}} and {{expired_section}} are filled with the
// blocks the summary builder laid out; when the builder produced nothing
// for a block, its marker is dropped, so that no empty heading remains.
func RenderSummary(text string, s Summary) string {
	values := map[string]string{
		"month":            s.Month,
		"count":            fmt.Sprint(s.Count),
		"items":            s.Items,
		"expired":          s.Expired,
		"expired_count":    fmt.Sprint(s.ExpiredCount),
		"upcoming_section": s.UpcomingSection,
		"expired_section":  s.ExpiredSection,
	}
	return replace(text, values)
}

// RenderTitle replaces the markers in a title template. It accepts both the
// device markers and the summary markers, so that one title template works
// for every kind of reminder. A title rendered for a single device counts
// that device, so {{count}} never shows up empty.
func RenderTitle(text string, d Device, days int) string {
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
		"count":    "1",
	}
	return replace(text, values)
}

// RenderTitleSummary renders a title for a monthly reminder.
//
// A title may use the summary markers and the device markers, so both are
// filled here. That way a title never carries a raw marker out to the reader.
// The device markers come from the representative record of the summary.
func RenderTitleSummary(text string, s Summary) string {
	values := map[string]string{
		"month":         s.Month,
		"count":         fmt.Sprint(s.Count),
		"items":         s.Items,
		"expired":       s.Expired,
		"expired_count": fmt.Sprint(s.ExpiredCount),
		"project":       s.Device.Project,
		"customer":      s.Device.Customer,
		"contact":       s.Device.Contact,
		"vendor":        s.Device.Vendor,
		"model":         s.Device.Model,
		"sn":            s.Device.SN,
		"start":         s.Device.Start,
		"end":           s.Device.End,
		"days":          fmt.Sprint(s.Days),
		"notes":         s.Device.Notes,
		"owner":         s.Device.Owner,
		"device":        strings.TrimSpace(s.Device.Vendor + " " + s.Device.Model),
	}
	return replace(text, values)
}

// ReplaceKeys swaps every marker it knows with the value the map holds.
// Unknown markers are left untouched so that a validation mistake stays
// visible in the preview. It is exported for the renderers outside the
// package that compose a title from their own values.
func ReplaceKeys(text string, values map[string]string) string {
	return replace(text, values)
}

// KnownMarker reports whether a text uses the marker with the key given.
func KnownMarker(text, key string) bool {
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		if m[1] == key {
			return true
		}
	}
	return false
}

// replace swaps every known marker. Unknown markers are left untouched so
// that a validation mistake stays visible in the preview.
func replace(text string, values map[string]string) string {
	return markerPattern.ReplaceAllStringFunc(text, func(m string) string {
		sub := markerPattern.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := values[sub[1]]; ok {
			return v
		}
		return m
	})
}

// Sample renders a template with the sample values of its kind.
func Sample(kind, text string) string {
	markers := PlaceholdersFor(kind, false)
	values := map[string]string{}
	for _, p := range markers {
		values[p.Key] = p.Sample
	}
	return replace(text, values)
}

// SampleWithout renders a template with the sample values of its kind and
// leaves one marker out, so that the caller can fill it with a layout that
// belongs to the format of the message instead of a sample text.
func SampleWithout(kind, text, skip string) string {
	markers := PlaceholdersFor(kind, false)
	values := map[string]string{}
	for _, p := range markers {
		if p.Key == skip {
			continue
		}
		values[p.Key] = p.Sample
	}
	return replace(text, values)
}

// SampleTitle renders a title template with the sample values.
func SampleTitle(text string) string {
	values := map[string]string{}
	for _, p := range titleMarkers {
		values[p.Key] = p.Sample
	}
	return replace(text, values)
}

// ValidateTitle checks a title template. A title accepts both the device
// markers and the summary markers, so that one title template works for
// every kind of reminder.
func ValidateTitle(text string) error {
	unknown := []string{}
	seen := map[string]bool{}
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !KnownFor(KindMilestone, key, true) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	available := []string{}
	for _, p := range titleMarkers {
		available = append(available, "{{"+p.Key+"}}")
	}
	return fmt.Errorf("标题模板包含不支持的占位符：%s。可用：%s",
		strings.Join(unknown, "、"), strings.Join(available, "、"))
}

// ValidateItem checks a monthly item template.
func ValidateItem(text string) error {
	unknown := []string{}
	seen := map[string]bool{}
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !KnownItem(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	available := []string{}
	for _, p := range itemMarkers {
		available = append(available, "{{"+p.Key+"}}")
	}
	return fmt.Errorf("明细模板包含不支持的占位符：%s。可用：%s",
		strings.Join(unknown, "、"), strings.Join(available, "、"))
}

// KnownItem reports whether a marker is available in an item template.
func KnownItem(key string) bool {
	for _, p := range itemMarkers {
		if p.Key == key {
			return true
		}
	}
	return false
}

// RenderItem lays out one line of the monthly summary.
func RenderItem(text string, d Device, days int) string {
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
	}
	return replace(text, values)
}

// SampleItem renders an item template with the sample values.
func SampleItem(text string) string {
	values := map[string]string{}
	for _, p := range itemMarkers {
		values[p.Key] = p.Sample
	}
	return replace(text, values)
}

// Markers lists the markers used in a template, in the order they appear.
func Markers(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range markerPattern.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
