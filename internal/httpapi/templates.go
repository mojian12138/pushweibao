package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/cron"
	"maintenance-reminder/internal/systemlog"
	"maintenance-reminder/internal/template"
)

// The message templates. A template holds the words of a reminder: the
// title, the body and the line of a summary. The strategies point at the
// template they send, so the words and the triggers can be managed apart.
//
// The built-in templates carry the detailed window layout the program
// starts with; they can be viewed and copied but not deleted, so that the
// strategies of a fresh install always have a template to point at.

// PushTemplate is one message template.
type PushTemplate struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Format        string `json:"format"`
	TitleTemplate string `json:"title_template"`
	BodyTemplate  string `json:"body_template"`
	ItemTemplate  string `json:"item_template"`
	Builtin       bool   `json:"builtin"`
}

// validTemplateKind reports whether the kind of a template is one the
// program renders.
func validTemplateKind(kind string) bool {
	return kind == template.KindMilestone || kind == template.KindMonthly || kind == template.KindCycle
}

// cronDescribe names a cron expression in plain words, so the editor can
// show what a schedule means before it is saved.
func (s *Server) cronDescribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Expr string `json:"expr"`
	}
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	sched, err := cron.Parse(req.Expr)
	if err != nil {
		fail(w, 400, "VALIDATION", err.Error())
		return
	}
	ok(w, map[string]any{"describe": cron.Describe(req.Expr), "next": sched.Next(time.Now()).Format("2006-01-02 15:04")})
}

// createTemplate writes a new template.
func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveTemplate(w, r, 0, true)
}

// updateTemplate writes a template back.
func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveTemplate(w, r, templateID(r), false)
}

// deleteTemplateHandler removes a template the user wrote.
func (s *Server) deleteTemplateHandler(w http.ResponseWriter, r *http.Request) {
	s.deleteTemplate(w, r, templateID(r))
}

// resetTemplateHandler puts a built-in template back to its shipped words.
func (s *Server) resetTemplateHandler(w http.ResponseWriter, r *http.Request) {
	s.resetTemplate(w, r, templateID(r))
}

// listTemplates hands the templates to the interface, the built-in ones
// first, then the ones the user wrote.
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,name,kind,format,title_template,body_template,item_template,builtin FROM push_templates ORDER BY builtin DESC, name, id`)
	if e != nil {
		errorResponse(w, e)
		return
	}
	defer rows.Close()
	out := []PushTemplate{}
	for rows.Next() {
		var x PushTemplate
		var builtin int
		if e = rows.Scan(&x.ID, &x.Name, &x.Kind, &x.Format, &x.TitleTemplate, &x.BodyTemplate, &x.ItemTemplate, &builtin); e != nil {
			errorResponse(w, e)
			return
		}
		x.Builtin = builtin == 1
		x.Format = template.NormalizeFormat(x.Format)
		out = append(out, x)
	}
	ok(w, out)
}

// saveTemplate writes a template back. A built-in template can be edited,
// so that its layout can be adjusted without copying it first; deleting
// one is refused, because the fresh installs point at it.
func (s *Server) saveTemplate(w http.ResponseWriter, r *http.Request, id int64, create bool) {
	var x PushTemplate
	if decode(r, &x) != nil || strings.TrimSpace(x.Name) == "" {
		fail(w, 400, "VALIDATION", "模板名称必填")
		return
	}
	if !validTemplateKind(x.Kind) {
		fail(w, 400, "VALIDATION", "模板类型无效")
		return
	}
	// The body of a template has to be one the strategy of the same kind
	// can send, so the markers of the body are checked the way a strategy
	// body is.
	if e := template.Validate(x.Kind, x.BodyTemplate, false); e != nil {
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
	if x.Format != "" && !template.ValidFormat(x.Format) {
		fail(w, 400, "VALIDATION", "消息格式无效，可选：html、txt、markdown")
		return
	}
	x.Format = template.NormalizeFormat(x.Format)
	if create {
		res, e := s.db.Exec(`INSERT INTO push_templates(name,kind,format,title_template,body_template,item_template,builtin) VALUES(?,?,?,?,?,?,0)`, x.Name, x.Kind, x.Format, x.TitleTemplate, x.BodyTemplate, x.ItemTemplate)
		if e != nil {
			errorResponse(w, e)
			return
		}
		x.ID, _ = res.LastInsertId()
		s.auditMutation(r, systemlog.CategoryStrategy, "template_created", "template", x.ID, "模板="+x.Name+" 类型="+x.Kind+" 格式="+x.Format)
	} else {
		var builtin int
		if e := s.db.QueryRow(`SELECT builtin FROM push_templates WHERE id=?`, id).Scan(&builtin); e != nil {
			fail(w, 404, "NOT_FOUND", "模板不存在")
			return
		}
		res, e := s.db.Exec(`UPDATE push_templates SET name=?,kind=?,format=?,title_template=?,body_template=?,item_template=? WHERE id=?`, x.Name, x.Kind, x.Format, x.TitleTemplate, x.BodyTemplate, x.ItemTemplate, id)
		if e != nil {
			errorResponse(w, e)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "NOT_FOUND", "模板不存在")
			return
		}
		x.ID = id
		x.Builtin = builtin == 1
		s.auditMutation(r, systemlog.CategoryStrategy, "template_updated", "template", id, "模板="+x.Name+" 类型="+x.Kind+" 格式="+x.Format)
	}
	ok(w, x)
}

// deleteTemplate removes a template the user wrote. A built-in template is
// kept, and a template a strategy points at is refused, because deleting
// it would leave the strategy without words to send.
func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request, id int64) {
	var builtin int
	var name string
	if e := s.db.QueryRow(`SELECT builtin,name FROM push_templates WHERE id=?`, id).Scan(&builtin, &name); e != nil {
		fail(w, 404, "NOT_FOUND", "模板不存在")
		return
	}
	if builtin == 1 {
		fail(w, 400, "VALIDATION", "内置模板不能删除，可以编辑或先复制一份")
		return
	}
	var used int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM strategies WHERE template_id=?`, id).Scan(&used); e != nil {
		errorResponse(w, e)
		return
	}
	if used > 0 {
		fail(w, 400, "VALIDATION", "有策略正在使用该模板，先把策略改回自带模板再删除")
		return
	}
	if _, e := s.db.Exec(`DELETE FROM push_templates WHERE id=?`, id); e != nil {
		errorResponse(w, e)
		return
	}
	s.auditMutation(r, systemlog.CategoryStrategy, "template_deleted", "template", id, "模板="+name)
	ok(w, map[string]any{"deleted": true})
}

// templateIDOf reads the id of a path value, so the router can hand the
// handler a number.
func templateID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// resetTemplate puts a built-in template back to the words the program
// shipped with, so that an edit gone wrong can be undone without a
// reinstall.
func (s *Server) resetTemplate(w http.ResponseWriter, r *http.Request, id int64) {
	var name, kind, format, title, body, item string
	e := s.db.QueryRow(`SELECT name,kind,format,title_template,body_template,item_template FROM push_templates WHERE id=?`, id).Scan(&name, &kind, &format, &title, &body, &item)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "模板不存在")
		return
	}
	if _, e = s.db.Exec(`UPDATE push_templates SET title_template=?,body_template=?,item_template=? WHERE id=?`,
		template.DefaultTitleFor(format, kind), template.DefaultBodyFor(format, kind), defaultItemForKind(format, kind), id); e != nil {
		errorResponse(w, e)
		return
	}
	s.auditMutation(r, systemlog.CategoryStrategy, "template_reset", "template", id, "模板="+name)
	ok(w, map[string]any{"reset": true})
}

// defaultItemForKind returns the item line a kind carries, empty for the
// kinds that do not lay a summary out.
func defaultItemForKind(format, kind string) string {
	if kind == template.KindMonthly {
		return template.DefaultItemFor(format)
	}
	return ""
}
