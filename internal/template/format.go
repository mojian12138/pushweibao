package template

import (
	"fmt"
	"strings"
)

// Message formats. They decide how the text of a message is written and how
// it is shown on the phone.
//
// The three names are the ones PushPlus uses for its own templates, so the
// format of a message can be handed to the API as it is.
const (
	// FormatHTML is a text with HTML tags.
	FormatHTML = "html"
	// FormatTxt is a plain text. The line breaks are written with \n.
	FormatTxt = "txt"
	// FormatMarkdown is a text written in markdown. It is the default,
	// because a markdown message is shown with the cleanest layout.
	FormatMarkdown = "markdown"
)

// Formats lists the supported formats, in the order the editor offers them.
// Markdown comes first, because it is the format the messages default to.
var Formats = []string{FormatMarkdown, FormatHTML, FormatTxt}

// NormalizeFormat cleans up a format and falls back to the default one.
// Markdown is the default, because its messages render best in PushPlus.
func NormalizeFormat(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case FormatHTML, FormatTxt, FormatMarkdown:
		return f
	}
	return FormatMarkdown
}

// ValidFormat reports whether a format is one of the supported ones. The
// value is matched as it came in, because NormalizeFormat folds anything
// unknown into the default format.
func ValidFormat(f string) bool {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case FormatHTML, FormatTxt, FormatMarkdown:
		return true
	}
	return false
}

// FormatLabel gives a format a readable name, for the interface.
func FormatLabel(f string) string {
	switch NormalizeFormat(f) {
	case FormatTxt:
		return "纯文本"
	case FormatMarkdown:
		return "Markdown"
	}
	return "HTML"
}

// FormatHint explains what a format does, so that the editor can tell the
// user what the result will look like.
func FormatHint(f string) string {
	switch NormalizeFormat(f) {
	case FormatTxt:
		return "纯文本内容，不转义 HTML，换行使用 \\n。"
	case FormatMarkdown:
		return "内容基于 Markdown 格式展示，推荐使用。"
	}
	return "支持 HTML 文本。"
}

// FormatTemplate returns the PushPlus template that belongs to a format.
// The names are the same, but the mapping is kept in one place so that the
// two lists cannot drift apart.
func FormatTemplate(f string) string { return NormalizeFormat(f) }

// preset is one ready-made template for a format and a kind of reminder.
type preset struct {
	Title string
	Body  string
	Item  string
}

// The default titles, one per format.
//
// A title is shown by PushPlus as plain text, so it carries no markup: a
// tag written in a title would show up on the phone as the tag itself.
var presetTitles = map[string][]string{
	FormatHTML: {
		"【维保提醒】剩余 {{days}} 天",
		"【月度维保汇总】{{month}}",
	},
	FormatTxt: {
		"【维保提醒】剩余 {{days}} 天",
		"【月度维保汇总】{{month}}",
	},
	FormatMarkdown: {
		"【维保提醒】剩余 {{days}} 天 · 共 {{count}} 台",
		"【月度维保汇总】{{month}}",
	},
}

// The look of the messages.
//
// The colours follow the palette of the program itself: a teal for the
// brand, a blue for the information and a red for the days that are gone.
// They are written into the message, so that the phone shows the same
// colours as the preview.
const (
	// ColorBrand is the colour of the title and the frame.
	ColorBrand = "#197d77"
	// ColorInfo is the colour of the values.
	ColorInfo = "#33454b"
	// ColorWarn is the colour of the days that are left.
	ColorWarn = "#d39837"
	// ColorDanger is the colour of the days that are gone.
	ColorDanger = "#ae5260"
)

// The ready-made message bodies, one set per format and per kind of
// reminder.
//
// The html set is a card: a coloured title bar, a table of the fields and a
// note at the end. The markdown set is the same in markdown, so that a
// message that is read in a plain reader still looks tidy.
var presetBodies = map[string]map[string]string{
	FormatHTML: {
		KindMilestone: `<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
<div style="background:#197d77;color:#ffffff;padding:10px 14px;font-weight:bold;font-size:15px;">维保到期提醒</div>
<div style="padding:6px 14px;">
{{milestone_items}}
</div>
<div style="background:#f5f8f8;padding:10px 14px;color:#197d77;font-weight:bold;">请及时联系客户办理续保。</div>
</div>`,
		KindMonthly: `<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
<div style="background:#197d77;color:#ffffff;padding:10px 14px;font-weight:bold;font-size:15px;">{{month}} 维保到期汇总</div>
<div style="padding:12px 14px;">
<div style="margin-bottom:10px;">本月共有 <span style="color:#d39837;font-weight:bold;">{{count}}</span> 台设备即将到期。</div>
{{upcoming_section}}
{{expired_section}}
</div>
</div>`,
		KindCycle: `<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
<div style="background:#197d77;color:#ffffff;padding:10px 14px;font-weight:bold;font-size:15px;">周期维保提醒</div>
<table style="width:100%;border-collapse:collapse;">
<tr><td style="padding:6px 14px;color:#6b7c82;width:88px;">项目</td><td style="padding:6px 14px;">{{project}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">设备</td><td style="padding:6px 14px;">{{vendor}} {{model}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">SN</td><td style="padding:6px 14px;">{{sn}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">剩余</td><td style="padding:6px 14px;color:#d39837;font-weight:bold;">{{days}} 天</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">负责人</td><td style="padding:6px 14px;">{{owner}}</td></tr>
</table>
<div style="background:#f5f8f8;padding:10px 14px;color:#197d77;font-weight:bold;">请及时办理续保。</div>
</div>`,
	},
	FormatTxt: {
		KindMilestone: "【维保到期提醒】\n{{milestone_items}}\n\n请及时联系客户办理续保。",
		KindMonthly:   "【{{month}} 维保到期汇总】\n本月共有 {{count}} 台设备即将到期。\n\n{{upcoming_section}}\n\n{{expired_section}}",
		KindCycle:     "【周期维保提醒】\n项目：{{project}}\n设备：{{vendor}} {{model}}\nSN：{{sn}}\n剩余：{{days}} 天\n负责人：{{owner}}\n\n请及时办理续保。",
	},
	FormatMarkdown: {
		KindMilestone: `## 📌 维保到期提醒

{{milestone_items}}

> 请及时联系客户办理续保。`,
		KindMonthly: `## 📅 {{month}} 维保到期汇总

本月共有 **{{count}}** 台设备即将到期。

{{upcoming_section}}

{{expired_section}}

> 请及时核对设备状态，安排续保。`,
		KindCycle: `## 🔁 周期维保提醒

| 项目 | {{project}} |
| :--- | :--- |
| 客户 | {{customer}} |
| 设备 | {{vendor}} {{model}} |
| SN | {{sn}} |
| 到期 | {{end}} |
| **剩余** | **{{days}} 天** |
| 负责人 | {{owner}} |

> 请及时办理续保。`,
	},
}

// The ready-made layouts of one line of the monthly summary. The summary is
// a list, so every format needs its own. The markdown line is a row of the
// table of the summary.
var presetItems = map[string]string{
	FormatHTML:     `<tr><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{project}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{model}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{sn}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{end}}</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;color:#d39837;font-weight:bold;">{{days}} 天</td><td style="padding:5px 8px;border-top:1px solid #eef1f2;">{{owner}}</td></tr>`,
	FormatTxt:      "项目：{{project}}｜型号：{{model}}｜SN：{{sn}}｜到期：{{end}}｜剩余：{{days}} 天｜负责人：{{owner}}",
	FormatMarkdown: "| {{project}} | {{model}} | {{sn}} | {{end}} | **{{days}} 天** | {{owner}} |",
}

// DefaultMilestoneItemFor returns the layout of one device inside the
// grouped list of a milestone message. The markdown layout keeps the
// project and the device side by side, so that a list of devices stays
// readable on the phone.
func DefaultMilestoneItemFor(format string) string {
	if item, ok := compactItemTemplates[NormalizeFormat(format)]; ok {
		return item
	}
	return compactItemTemplates[FormatTxt]
}

// DefaultTitleFor returns the ready-made title of a format for a kind.
//
// The milestone title may carry {{days}} and {{count}}: they are filled
// with the tier that fired and the number of devices in the message. A
// title is shown by PushPlus as plain text, so it carries no markup.
func DefaultTitleFor(format, kind string) string {
	set := presetTitles[NormalizeFormat(format)]
	if set == nil {
		return ""
	}
	// The monthly summary has its own title, the other kinds share the
	// reminder one.
	if kind == KindMonthly {
		return set[1]
	}
	return set[0]
}

// DefaultBodyFor returns the ready-made message body of a format for a
// kind. It is the template the editor offers when a message is empty.
func DefaultBodyFor(format, kind string) string {
	set := presetBodies[NormalizeFormat(format)]
	if set == nil {
		set = presetBodies[FormatHTML]
	}
	if body, ok := set[kind]; ok {
		return body
	}
	return set[KindMilestone]
}

// DefaultItemFor returns the ready-made layout of one line of the monthly
// summary for a format.
func DefaultItemFor(format string) string {
	if item, ok := presetItems[NormalizeFormat(format)]; ok {
		return item
	}
	return presetItems[FormatHTML]
}

// Presets returns the ready-made templates of a format, so that the editor
// can offer them without hardcoding them.
func Presets(format string) map[string]string {
	format = NormalizeFormat(format)
	out := map[string]string{
		"milestone_title": DefaultTitleFor(format, KindMilestone),
		"milestone_body":  DefaultBodyFor(format, KindMilestone),
		"monthly_title":   DefaultTitleFor(format, KindMonthly),
		"monthly_body":    DefaultBodyFor(format, KindMonthly),
		"cycle_title":     DefaultTitleFor(format, KindCycle),
		"cycle_body":      DefaultBodyFor(format, KindCycle),
		"monthly_item":    DefaultItemFor(format),
	}
	return out
}

// FormatTemplateNames returns the names of the templates the editor offers
// for a format, in the order they should appear.
func FormatTemplateNames(format string) []string {
	return []string{
		"milestone_title",
		"milestone_body",
		"monthly_title",
		"monthly_body",
		"cycle_title",
		"cycle_body",
		"monthly_item",
	}
}

// PresetLabel names a preset for the interface.
func PresetLabel(name string) string {
	labels := map[string]string{
		"milestone_title": "里程碑 · 标题",
		"milestone_body":  "里程碑 · 正文",
		"monthly_title":   "月度汇总 · 标题",
		"monthly_body":    "月度汇总 · 正文",
		"cycle_title":     "周期循环 · 标题",
		"cycle_body":      "周期循环 · 正文",
		"monthly_item":    "月度汇总 · 明细行",
	}
	if l, ok := labels[name]; ok {
		return l
	}
	return name
}

// PresetBody returns the text of a named preset. An unknown name gives an
// empty text, so that the caller can tell that nothing was found.
func PresetBody(format, name string) (string, error) {
	p := Presets(format)
	body, ok := p[name]
	if !ok {
		return "", fmt.Errorf("没有名为 %s 的预设模板", name)
	}
	return body, nil
}
