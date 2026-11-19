package template

import (
	"fmt"
	"sort"
	"strings"
)

// The builders for reminders that cover many devices at once: a milestone
// reminder that puts every device of the window into one message, grouped
// by the tier its remaining days fall into, and a monthly summary that
// shows the devices that are about to expire next to the ones that already
// have.
//
// The builders only sort the devices and lay the blocks out; the words of a
// line come from the templates of the strategy, so an edited template keeps
// its own look.
//
// The markdown blocks keep a blank line between a heading and the table
// under it: a table row that follows a paragraph line directly is read as
// part of that paragraph, which is why the monthly summary of the earlier
// release showed its table folded into one line.

// MilestoneHit is one device of a milestone reminder: the record, its id
// for the queue and the days that are left. The tier the device belongs to
// is derived from the offsets of the strategy, so an edited offset list
// re-draws the tiers.
type MilestoneHit struct {
	Device Device
	ID     int64
	Days   int
	Offset int
}

// DefaultOffsets are the tiers the ready-made reminders group their
// devices into.
var DefaultOffsets = []int{90, 60, 30, 7, 0}

// The compact layouts of one device inside a milestone message, one per
// format. They are rendered through the item renderer, so the markers are
// the ones an item line accepts. The markdown layout is a row of the table
// of its tier.
var compactItemTemplates = map[string]string{
	FormatMarkdown: "| {{project}} | {{vendor}} {{model}} | {{sn}} | {{end}} | **{{days}} 天** | {{owner}} |",
	FormatHTML:     `<tr><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{project}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{vendor}} {{model}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{sn}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{end}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;color:#d39837;font-weight:bold;">{{days}} 天</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{owner}}</td></tr>`,
	FormatTxt:      "【{{days}} 天】项目：{{project}}｜设备：{{vendor}} {{model}}｜SN：{{sn}}｜到期：{{end}}｜负责人：{{owner}}",
}

// The table head that belongs to the layout of a milestone tier, and the
// tag that closes it.
func compactTableParts(format string) (head, closer string) {
	switch NormalizeFormat(format) {
	case FormatMarkdown:
		return "| 项目 | 设备 | SN | 到期 | 剩余 | 负责人 |\n| :--- | :--- | :--- | :--- | :--- | :--- |", ""
	case FormatHTML:
		return `<table style="width:100%;border-collapse:collapse;font-size:13px;"><tr style="background:#f5f8f8;color:#6b7c82;"><th style="padding:6px 8px;text-align:left;">项目</th><th style="padding:6px 8px;text-align:left;">设备</th><th style="padding:6px 8px;text-align:left;">SN</th><th style="padding:6px 8px;text-align:left;">到期</th><th style="padding:6px 8px;text-align:left;">剩余</th><th style="padding:6px 8px;text-align:left;">负责人</th></tr>`, "</table>"
	}
	return "", ""
}

// milestoneTiers returns the offsets that draw the tiers of a milestone
// message, sorted ascending and without duplicates. The devices of the
// window are grouped into these tiers.
func milestoneTiers(offsets []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, o := range offsets {
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	sort.Ints(out)
	return out
}

// milestoneTierOf names the tier a device belongs to: the smallest offset
// that still covers its remaining days, or the expired tier for a device
// that ran out already.
func milestoneTierOf(tiers []int, days int) int {
	if days < 0 {
		return -1
	}
	for _, o := range tiers {
		if days <= o {
			return o
		}
	}
	return tiers[len(tiers)-1]
}

// MilestoneTiers returns the offsets that draw the tiers of a milestone
// message, sorted ascending and without duplicates. The scheduler uses it
// to name the tiers in the trigger point of the message.
func MilestoneTiers(offsets []int) []int { return milestoneTiers(offsets) }

// milestoneHeading writes the heading of one tier. The tiers that need
// attention today carry a different sign, so that they stand out in the
// list; the label says the range the tier covers, because the devices of a
// tier do not share the exact day.
func milestoneHeading(format string, tier, count int) string {
	label, icon := fmt.Sprintf("剩余 ≤%d 天", tier), "⏰"
	if tier == 0 {
		label, icon = "今日到期", "🔴"
	}
	if tier < 0 {
		label, icon = "已过期", "⛔"
	}
	switch NormalizeFormat(format) {
	case FormatMarkdown:
		return fmt.Sprintf("### %s %s · %d 台", icon, label, count)
	case FormatHTML:
		return fmt.Sprintf(`<h3 style="margin:10px 0 6px;color:#197d77;font-size:14px;">%s %s · %d 台</h3>`, icon, label, count)
	}
	return fmt.Sprintf("【%s · %d 台】", label, count)
}

// compactItemLine renders one device of a milestone message.
func compactItemLine(format string, d Device, days int) string {
	t := compactItemTemplates[NormalizeFormat(format)]
	if t == "" {
		t = compactItemTemplates[FormatTxt]
	}
	return strings.TrimRight(RenderItem(t, d, days), "\n")
}

// BuildMilestoneItems lays the devices of the window out for a milestone
// message: the tiers come in urgency order — the expired ones first, then
// today, then the wider ranges — every tier gets a heading of its own, and
// inside a tier the devices are ordered by the days that are left, so that
// the most urgent one is read first.
func BuildMilestoneItems(format string, offsets []int, hits []MilestoneHit) string {
	tiers := milestoneTiers(offsets)
	if len(tiers) == 0 {
		tiers = []int{0}
	}
	sorted := make([]MilestoneHit, len(hits))
	copy(sorted, hits)
	// The devices are ordered by urgency, so that the message reads from
	// the most urgent tier down to the widest one.
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, tj := milestoneTierOf(tiers, sorted[i].Days), milestoneTierOf(tiers, sorted[j].Days)
		if ti != tj {
			return ti < tj
		}
		if sorted[i].Days != sorted[j].Days {
			return sorted[i].Days < sorted[j].Days
		}
		return sorted[i].Device.End < sorted[j].Device.End
	})
	var out strings.Builder
	for i := 0; i < len(sorted); {
		j := i
		for j < len(sorted) && milestoneTierOf(tiers, sorted[j].Days) == milestoneTierOf(tiers, sorted[i].Days) {
			j++
		}
		group := sorted[i:j]
		tier := milestoneTierOf(tiers, group[0].Days)
		// The blank line after the heading keeps the heading and the table
		// apart, because a table row that follows a paragraph line without
		// a blank line between them is read as part of that paragraph and
		// the whole table folds into one line.
		out.WriteString(milestoneHeading(format, tier, len(group)) + "\n\n")
		if head, closer := compactTableParts(format); head != "" {
			out.WriteString(head + "\n")
			for _, h := range group {
				out.WriteString(compactItemLine(format, h.Device, h.Days) + "\n")
			}
			if closer != "" {
				out.WriteString(closer + "\n")
			}
		} else {
			for _, h := range group {
				out.WriteString(compactItemLine(format, h.Device, h.Days) + "\n")
			}
		}
		out.WriteString("\n")
		i = j
	}
	return strings.TrimRight(out.String(), "\n")
}

// SummaryRow is one device of a summary with the days that are left.
type SummaryRow struct {
	Device Device
	Days   int
}

// absDays returns the distance to the end date, so that a device that
// expired yesterday and one that expires tomorrow can be ordered by the
// same yardstick: the closer to the date, the more urgent.
func absDays(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// sortSummaryRows orders the devices of a summary by urgency: the closer to
// the end date, the earlier the device is shown, so that an overdue device
// that just slipped past sits at the top of its block.
func sortSummaryRows(rows []SummaryRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ai, aj := absDays(rows[i].Days), absDays(rows[j].Days)
		if ai != aj {
			return ai < aj
		}
		return rows[i].Device.End < rows[j].Device.End
	})
}

// BuildSummaryLines splits the devices of a summary into the lines of the
// two blocks, ordered by urgency. The lines carry no heading, so they fit
// the {{items}} and {{expired}} markers of a hand-written template.
func BuildSummaryLines(itemTemplate string, rows []SummaryRow) (upcoming, expired string) {
	sorted := make([]SummaryRow, len(rows))
	copy(sorted, rows)
	sortSummaryRows(sorted)
	var run, gone strings.Builder
	for _, r := range sorted {
		line := strings.TrimRight(RenderItem(itemTemplate, r.Device, r.Days), "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if r.Days < 0 {
			gone.WriteString(line + "\n")
		} else {
			run.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(run.String(), "\n"), strings.TrimRight(gone.String(), "\n")
}

// summaryHeading writes the heading of one block of a summary.
func summaryHeading(format, kind string, count int) string {
	label, icon := "即将到期", "🔔"
	if kind == "expired" {
		label, icon = "已过期", "⛔"
	}
	switch NormalizeFormat(format) {
	case FormatMarkdown:
		return fmt.Sprintf("### %s %s · %d 台", icon, label, count)
	case FormatHTML:
		return fmt.Sprintf(`<p style="margin:12px 0 6px;font-weight:bold;">%s %s · %d 台</p>`, icon, label, count)
	}
	return fmt.Sprintf("【%s · %d 台】", label, count)
}

// summaryTableParts returns the table head that fits the ready-made item
// line of a format, together with the tag that closes the table.
func summaryTableParts(format string) (head, closer string) {
	switch NormalizeFormat(format) {
	case FormatMarkdown:
		return "| 项目 | 型号 | SN | 到期 | 剩余 | 负责人 |\n| :--- | :--- | :--- | :--- | :--- | :--- |", ""
	case FormatHTML:
		return `<table style="width:100%;border-collapse:collapse;font-size:13px;"><tr style="background:#f5f8f8;color:#6b7c82;"><th style="padding:6px 8px;text-align:left;">项目</th><th style="padding:6px 8px;text-align:left;">型号</th><th style="padding:6px 8px;text-align:left;">SN</th><th style="padding:6px 8px;text-align:left;">到期</th><th style="padding:6px 8px;text-align:left;">剩余</th><th style="padding:6px 8px;text-align:left;">负责人</th></tr>`, "</table>"
	}
	return "", ""
}

// isDefaultItem reports whether the item template is the ready-made one of
// the format, so that the block can carry the table head that fits it. A
// template the user wrote is laid out without a head, because the code
// cannot know what columns it holds.
func isDefaultItem(format, item string) bool {
	item = strings.TrimSpace(item)
	return item == "" || item == strings.TrimSpace(DefaultItemFor(format))
}

// summarySection writes one block of a summary: a heading, and under it the
// devices of the block. A block without devices is empty, so that a month
// without overdue devices shows no expired heading at all.
func summarySection(format, kind, itemTemplate string, rows []SummaryRow) string {
	if len(rows) == 0 {
		return ""
	}
	var out strings.Builder
	// The blank line after the heading keeps the heading and the table
	// apart, because a table row that follows a paragraph line without a
	// blank line between them is read as part of that paragraph and the
	// whole table folds into one line.
	out.WriteString(summaryHeading(format, kind, len(rows)) + "\n\n")
	item := strings.TrimSpace(itemTemplate)
	if item == "" {
		item = DefaultItemFor(format)
	}
	head, closer := summaryTableParts(format)
	if head != "" && isDefaultItem(format, item) {
		out.WriteString(head + "\n")
		for _, r := range rows {
			out.WriteString(strings.TrimRight(RenderItem(item, r.Device, r.Days), "\n") + "\n")
		}
		if closer != "" {
			out.WriteString(closer + "\n")
		}
	} else {
		for _, r := range rows {
			out.WriteString(strings.TrimRight(RenderItem(item, r.Device, r.Days), "\n") + "\n")
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

// BuildSummaryBlocks builds the two blocks of a monthly summary: the
// devices that are about to expire, then the ones that already have. Each
// block is headed by a line that names the count, and the devices inside
// are ordered by urgency. A block without devices comes back empty, so the
// message carries no empty heading.
func BuildSummaryBlocks(format, itemTemplate string, rows []SummaryRow) (upcoming, expired string) {
	sorted := make([]SummaryRow, len(rows))
	copy(sorted, rows)
	sortSummaryRows(sorted)
	var up, gone []SummaryRow
	for _, r := range sorted {
		if r.Days < 0 {
			gone = append(gone, r)
		} else {
			up = append(up, r)
		}
	}
	return summarySection(format, "upcoming", itemTemplate, up), summarySection(format, "expired", itemTemplate, gone)
}

// MilestoneItemsForOne lays the grouped list out for a message that covers
// a single device, so that a preview of a one-device message shows the same
// layout the real message has.
func MilestoneItemsForOne(format string, d Device, days int) string {
	return BuildMilestoneItems(format, DefaultOffsets, []MilestoneHit{{Device: d, Days: days}})
}

// SampleDevice returns the device the sample values describe, so that a
// preview with sample data can fill the markers of a single device too.
func SampleDevice() Device {
	d := Device{}
	for _, p := range deviceMarkers {
		switch p.Key {
		case "project":
			d.Project = p.Sample
		case "customer":
			d.Customer = p.Sample
		case "contact":
			d.Contact = p.Sample
		case "vendor":
			d.Vendor = p.Sample
		case "model":
			d.Model = p.Sample
		case "sn":
			d.SN = p.Sample
		case "start":
			d.Start = p.Sample
		case "end":
			d.End = p.Sample
		case "notes":
			d.Notes = p.Sample
		case "owner":
			d.Owner = p.Sample
		}
	}
	return d
}

// SampleMilestoneItems lays the grouped list out with the sample device, so
// that a preview with sample data shows the layout of the phone.
func SampleMilestoneItems(format string) string {
	return MilestoneItemsForOne(format, SampleDevice(), 30)
}

// SampleSummarySections builds the two blocks of a monthly summary from the
// sample device, so that a preview with sample data shows the layout of the
// message. One device is shown as about to expire, one as already overdue.
func SampleSummarySections(format, itemTemplate string) (upcoming, expired string) {
	device := SampleDevice()
	overdue := device
	overdue.End = "2026-10-01"
	return BuildSummaryBlocks(format, itemTemplate, []SummaryRow{
		{Device: device, Days: 30},
		{Device: device, Days: 7},
		{Device: overdue, Days: -5},
	})
}
