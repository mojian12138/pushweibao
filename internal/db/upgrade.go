package db

import (
	"database/sql"
	"fmt"
	"strings"

	"maintenance-reminder/internal/template"
)

// The message bodies that the first release wrote into the database. A
// strategy that still carries one of them has never been edited by the user,
// so it can be moved to the html preset without losing a word that someone
// wrote.
//
// The monthly body of that release was written into the database with a
// literal "\n" — the seed statement put the two characters into the row
// instead of a line break — so both spellings are recognised.
var legacyStrategyBodies = map[string][]string{
	template.KindMilestone: {
		"【维保提醒】剩余 {{days}} 天｜项目：{{project}}｜客户：{{customer}}｜设备：{{vendor}} {{model}}｜SN：{{sn}}｜到期：{{end}}｜负责人：{{owner}}",
	},
	template.KindMonthly: {
		"【月度维保汇总】{{month}}：{{count}} 台设备即将到期\\n{{items}}",
		"【月度维保汇总】{{month}}：{{count}} 台设备即将到期\n{{items}}",
	},
	template.KindCycle: {
		"【维保提醒】项目：{{project}}｜{{vendor}} {{model}}（SN：{{sn}}）剩余 {{days}} 天，请及时续保。",
	},
}

// isLegacyBody reports whether a body is one the first release wrote.
func isLegacyBody(kind, body string) bool {
	for _, legacy := range legacyStrategyBodies[kind] {
		if strings.TrimSpace(body) == legacy {
			return true
		}
	}
	return false
}

// upgradeLegacyStrategies moves the strategies that no one has edited from
// the plain text of the first release to the html preset, so that a program
// that is started on an old database sends styled messages without the user
// having to edit every strategy. A strategy whose body was changed is left
// alone, because that text belongs to the user; a title or an item that
// already has a value is kept as it is.
func upgradeLegacyStrategies(database *sql.DB) error {
	rows, err := database.Query(`SELECT id,type,template,title_template,item_template FROM strategies`)
	if err != nil {
		return err
	}
	type candidate struct {
		id          int64
		kind        string
		title, item string
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
		todo = append(todo, candidate{id: id, kind: kind, title: title, item: item})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range todo {
		body := template.DefaultBodyFor(template.FormatHTML, x.kind)
		title := strings.TrimSpace(x.title)
		if title == "" {
			title = template.DefaultTitleFor(template.FormatHTML, x.kind)
		}
		item := x.item
		if x.kind == template.KindMonthly && strings.TrimSpace(item) == "" {
			item = template.DefaultItemFor(template.FormatHTML)
		}
		if _, err = database.Exec(`UPDATE strategies SET template=?,title_template=?,item_template=?,format=? WHERE id=?`, body, title, item, template.FormatHTML, x.id); err != nil {
			return fmt.Errorf("upgrade strategy %d: %w", x.id, err)
		}
	}
	return nil
}
