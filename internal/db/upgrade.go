package db

import (
	"database/sql"
	"fmt"
	"strings"

	"maintenance-reminder/internal/template"
)

// The message bodies that earlier releases wrote into the database. A
// strategy that still carries one of them has never been edited by the
// user, so it can be moved to the preset of the current release without
// losing a word that someone wrote.
//
// The list grows with every release that changes the layout of the
// ready-made templates, so that the strategies the program itself filled in
// keep following the current look.
var legacyStrategyBodies = map[string][]string{
	template.KindMilestone: {
		// The plain text of the first release.
		"【维保提醒】剩余 {{days}} 天｜项目：{{project}}｜客户：{{customer}}｜设备：{{vendor}} {{model}}｜SN：{{sn}}｜到期：{{end}}｜负责人：{{owner}}",
		// The html card of the styled release, before the grouped list.
		`<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
<div style="background:#197d77;color:#ffffff;padding:10px 14px;font-weight:bold;font-size:15px;">维保到期提醒</div>
<table style="width:100%;border-collapse:collapse;">
<tr><td style="padding:6px 14px;color:#6b7c82;width:88px;">项目</td><td style="padding:6px 14px;">{{project}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">客户</td><td style="padding:6px 14px;">{{customer}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">设备</td><td style="padding:6px 14px;">{{vendor}} {{model}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">SN</td><td style="padding:6px 14px;">{{sn}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">到期</td><td style="padding:6px 14px;">{{end}}</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">剩余</td><td style="padding:6px 14px;color:#d39837;font-weight:bold;">{{days}} 天</td></tr>
<tr><td style="padding:6px 14px;color:#6b7c82;">负责人</td><td style="padding:6px 14px;">{{owner}}</td></tr>
</table>
<div style="background:#f5f8f8;padding:10px 14px;color:#197d77;font-weight:bold;">请及时联系客户办理续保。</div>
</div>`,
		// The markdown preset before the devices were grouped by tier.
		`## 📌 维保到期提醒

| 项目 | {{project}} |
| :--- | :--- |
| 客户 | {{customer}} |
| 设备 | {{vendor}} {{model}} |
| SN | {{sn}} |
| 到期 | {{end}} |
| **剩余** | **{{days}} 天** |
| 负责人 | {{owner}} |

> 请及时联系客户办理续保。`,
		// The markdown preset of the grouped release that drew tables under
		// a bold heading. The heading folded the table into one line, so
		// the current preset writes the tiers as headings of their own.
		"## 📌 维保到期提醒\n\n{{milestone_items}}\n> 请及时联系客户办理续保。",
		"## 📌 维保到期提醒\n\n{{milestone_items}}\n\n> 请及时联系客户办理续保。",
	},
	template.KindMonthly: {
		// The plain text of the first release. The body was written into
		// the database with a literal "\n" — the seed statement put the
		// two characters into the row instead of a line break — so both
		// spellings are recognised.
		"【月度维保汇总】{{month}}：{{count}} 台设备即将到期\\n{{items}}",
		"【月度维保汇总】{{month}}：{{count}} 台设备即将到期\n{{items}}",
		// The html preset before the summary was split into sections.
		`<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
<div style="background:#197d77;color:#ffffff;padding:10px 14px;font-weight:bold;font-size:15px;">{{month}} 维保到期汇总</div>
<div style="padding:12px 14px;">
<div style="margin-bottom:10px;">本月共有 <span style="color:#d39837;font-weight:bold;">{{count}}</span> 台设备即将到期。</div>
<table style="width:100%;border-collapse:collapse;font-size:13px;">
<tr style="background:#f5f8f8;color:#6b7c82;"><th style="padding:6px 8px;text-align:left;">项目</th><th style="padding:6px 8px;text-align:left;">型号</th><th style="padding:6px 8px;text-align:left;">SN</th><th style="padding:6px 8px;text-align:left;">到期</th><th style="padding:6px 8px;text-align:left;">剩余</th><th style="padding:6px 8px;text-align:left;">负责人</th></tr>
{{items}}
</table>
</div>
</div>`,
		// The markdown preset before the summary was split into sections.
		`## 📅 {{month}} 维保到期汇总

本月共有 **{{count}}** 台设备即将到期。

| 项目 | 型号 | SN | 到期 | 剩余 | 负责人 |
| :--- | :--- | :--- | :--- | :--- | :--- |
{{items}}`,
		// The markdown preset of the sectioned release that drew a table
		// under a bold heading. The heading folded the table into one
		// line, so the current preset writes the sections as headings of
		// their own.
		`## 📅 {{month}} 维保到期汇总

本月共有 **{{count}}** 台设备即将到期。

{{upcoming_section}}

{{expired_section}}

> 请及时核对设备状态，安排续保。`,
	},
	template.KindCycle: {
		"【维保提醒】项目：{{project}}｜{{vendor}} {{model}}（SN：{{sn}}）剩余 {{days}} 天，请及时续保。",
		// The html card of the styled release. It is the text the upgrade
		// of that release wrote into the strategies, so a cycle strategy
		// that carries it was never touched by the user either.
		`<div style="border:1px solid #e3e8ea;border-radius:10px;overflow:hidden;font-size:14px;color:#33454b;">
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
		// The markdown preset of the first markdown release, drawn as a
		// table. The current preset uses list lines.
		`## 🔁 周期维保提醒

| 项目 | {{project}} |
| :--- | :--- |
| 设备 | {{vendor}} {{model}} |
| SN | {{sn}} |
| **剩余** | **{{days}} 天** |
| 负责人 | {{owner}} |

> 请及时办理续保。`,
	},
}

// The titles the earlier releases filled in. A title that still carries one
// of them follows the preset of the current release as well.
var legacyStrategyTitles = []string{
	"",
	"【维保提醒】剩余 {{days}} 天",
	"【月度维保汇总】{{month}}",
}

// isLegacyBody reports whether a body is one an earlier release wrote.
func isLegacyBody(kind, body string) bool {
	for _, legacy := range legacyStrategyBodies[kind] {
		if strings.TrimSpace(body) == strings.TrimSpace(legacy) {
			return true
		}
	}
	return false
}

// isLegacyTitle reports whether a title is one an earlier release filled in
// or left empty, so that the upgrade may replace it with the current one.
func isLegacyTitle(title string) bool {
	for _, legacy := range legacyStrategyTitles {
		if strings.TrimSpace(title) == strings.TrimSpace(legacy) {
			return true
		}
	}
	return false
}

// isLegacyItem reports whether the line layout of a summary is one an
// earlier release filled in or left empty.
func isLegacyItem(item string) bool {
	switch strings.TrimSpace(item) {
	case "", template.DefaultItemFor(template.FormatHTML), template.DefaultItemFor(template.FormatTxt),
		// The list line of the release that mistook the folded table of
		// PushPlus for a missing table support. The current preset draws a
		// table again, so the line follows it back.
		"- **{{days}} 天**｜{{project}}｜{{model}}｜SN {{sn}}｜到期 {{end}}｜负责人 {{owner}}":
		return true
	}
	return false
}

// upgradeLegacyStrategies moves the strategies that no one has edited from
// the text of an earlier release to the preset of the current release, so
// that a program that is started on an old database sends the messages of
// the current look without the user having to edit every strategy.
//
// Markdown is the format the messages default to, so the preset the
// upgrade writes is the markdown one: its messages render best in PushPlus,
// and the milestone reminder of the current release groups the devices of a
// day by the tier they fired on, which only the current presets ask for.
//
// A strategy whose body was changed is left alone, because that text
// belongs to the user; a title or an item that the user changed is kept as
// it is as well.
func upgradeLegacyStrategies(database *sql.DB) error {
	rows, err := database.Query(`SELECT id,type,template,title_template,item_template FROM strategies`)
	if err != nil {
		return err
	}
	type candidate struct {
		id   int64
		kind string
	}
	var todo []candidate
	for rows.Next() {
		var id int64
		var kind, body, title, item string
		if err = rows.Scan(&id, &kind, &body, &title, &item); err != nil {
			rows.Close()
			return err
		}
		if _, ok := legacyStrategyBodies[kind]; !ok {
			continue
		}
		// Only a strategy that still carries the old text is touched.
		if !isLegacyBody(kind, body) {
			continue
		}
		todo = append(todo, candidate{id: id, kind: kind})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range todo {
		body := template.DefaultBodyFor(template.FormatMarkdown, x.kind)
		title := template.DefaultTitleFor(template.FormatMarkdown, x.kind)
		item := ""
		if x.kind == template.KindMonthly && isLegacyItem(readItem(database, x.id)) {
			item = template.DefaultItemFor(template.FormatMarkdown)
		}
		if _, err = database.Exec(`UPDATE strategies SET template=?,title_template=COALESCE(NULLIF(?,''),title_template),item_template=CASE WHEN ?<>'' THEN ? ELSE item_template END,format=? WHERE id=?`, body, title, item, item, template.FormatMarkdown, x.id); err != nil {
			return fmt.Errorf("upgrade strategy %d: %w", x.id, err)
		}
	}
	return nil
}

// readItem reads the item template of a strategy, so that the upgrade can
// tell whether the user changed it.
func readItem(database *sql.DB, id int64) string {
	var item string
	if err := database.QueryRow(`SELECT item_template FROM strategies WHERE id=?`, id).Scan(&item); err != nil {
		return ""
	}
	return item
}
