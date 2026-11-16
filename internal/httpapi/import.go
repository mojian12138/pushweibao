package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"maintenance-reminder/internal/importexport"
	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/systemlog"
)

func uploadedFile(r *http.Request) (multipart.File, error) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		return nil, err
	}
	f, _, err := r.FormFile("file")
	return f, err
}
func (s *Server) deviceTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=devices-template.xlsx")
	if e := importexport.WriteTemplate(w, importexport.DeviceColumns); e != nil {
		errorResponse(w, e)
	}
}
func (s *Server) exportProjects(w http.ResponseWriter, r *http.Request) {
	f := excelize.NewFile()
	defer f.Close()
	for i, column := range importexport.ProjectColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("Sheet1", cell, column)
	}
	rows, err := s.db.Query(`SELECT name,customer,contact,contract_no,notes FROM projects ORDER BY name`)
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer rows.Close()
	rowNumber := 2
	for rows.Next() {
		var values [5]string
		ptr := make([]any, len(values))
		for i := range values {
			ptr[i] = &values[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			errorResponse(w, err)
			return
		}
		for i, value := range values {
			cell, _ := excelize.CoordinatesToCellName(i+1, rowNumber)
			_ = f.SetCellValue("Sheet1", cell, value)
		}
		rowNumber++
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=projects.xlsx")
	if err = f.Write(w); err != nil {
		errorResponse(w, err)
	}
}
func (s *Server) importProjects(w http.ResponseWriter, r *http.Request) {
	f, e := uploadedFile(r)
	if e != nil {
		fail(w, 400, "IMPORT_FILE", "请选择 Excel 文件")
		return
	}
	defer f.Close()
	rows, e := importexport.ReadXLSX(f, importexport.ProjectColumns[:1])
	if e != nil {
		fail(w, 400, "IMPORT_FILE", e.Error())
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		errorResponse(w, e)
		return
	}
	result := importexport.Result{Errors: []importexport.RowError{}}
	for _, row := range rows {
		v := row.Values
		if strings.TrimSpace(v["项目名"]) == "" {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: "项目名不能为空"})
			continue
		}
		if _, e = tx.Exec(`INSERT INTO projects(name,customer,contact,contract_no,notes) VALUES(?,?,?,?,?)`, v["项目名"], v["客户"], v["联系人"], v["合同号"], v["备注"]); e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: e.Error()})
			continue
		}
		result.Imported++
	}
	if e = tx.Commit(); e != nil {
		errorResponse(w, e)
		return
	}
	ok(w, result)
}
func (s *Server) importDevices(w http.ResponseWriter, r *http.Request) {
	f, e := uploadedFile(r)
	if e != nil {
		fail(w, 400, "IMPORT_FILE", "请选择 Excel 文件")
		return
	}
	defer f.Close()
	rows, e := importexport.ReadXLSX(f, importexport.DeviceColumns[:6])
	if e != nil {
		fail(w, 400, "IMPORT_FILE", e.Error())
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		errorResponse(w, e)
		return
	}
	result := importexport.Result{Errors: []importexport.RowError{}}
	for _, row := range rows {
		v := row.Values
		var projectID int64
		e = tx.QueryRow(`SELECT id FROM projects WHERE name=? ORDER BY id LIMIT 1`, v["项目"]).Scan(&projectID)
		d := Device{ProjectID: FlexID(projectID), Vendor: v["厂商"], Model: v["型号"], Serial: v["SN"], Start: v["维保开始"], End: v["维保结束"], Status: map[string]string{"维保中": "active", "已续保": "renewed", "已下架": "retired"}[v["状态"]], Owner: v["负责人"], ContractNo: v["合同号"], Notes: v["备注"]}
		if e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: "项目不存在"})
			continue
		}
		if e = validateDevice(d); e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: e.Error()})
			continue
		}
		_, e = tx.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,owner,contract_no,notes) VALUES(?,?,?,?,?,?,?,?,?,?)`, d.ProjectID, d.Vendor, d.Model, d.Serial, d.Start, d.End, defaultStatus(d.Status), d.Owner, d.ContractNo, d.Notes)
		if e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: "导入失败：SN 可能重复"})
			continue
		}
		result.Imported++
	}
	if e = tx.Commit(); e != nil {
		errorResponse(w, e)
		return
	}
	ok(w, result)
}

var deviceImportColumns = []string{"厂商", "型号", "SN", "维保开始", "维保结束", "状态", "负责人", "备注"}

func (s *Server) projectDeviceTemplate(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "项目编号无效")
		return
	}
	var name, contract string
	if e = s.db.QueryRow(`SELECT name,contract_no FROM projects WHERE id=?`, id).Scan(&name, &contract); e != nil {
		fail(w, 404, "NOT_FOUND", "项目不存在")
		return
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	// Row 1: project metadata (prefilled, read-only for the operator).
	info := []string{"项目名称", name, "合同号", contract}
	for i, v := range info {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, v)
	}
	_ = f.SetSheetName(sheet, "设备导入")
	for i, column := range deviceImportColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		_ = f.SetCellValue("设备导入", cell, column)
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename="+fmt.Sprintf("%s-devices-template.xlsx", url.PathEscape(name)))
	if e = f.Write(w); e != nil {
		errorResponse(w, e)
	}
}

func (s *Server) importProjectDevices(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "项目编号无效")
		return
	}
	var contract string
	var exists int
	if e = s.db.QueryRow(`SELECT COUNT(*),COALESCE(contract_no,'') FROM projects WHERE id=?`, id).Scan(&exists, &contract); e != nil || exists == 0 {
		fail(w, 404, "NOT_FOUND", "项目不存在")
		return
	}
	f, e := uploadedFile(r)
	if e != nil {
		fail(w, 400, "IMPORT_FILE", "请选择 Excel 文件")
		return
	}
	defer f.Close()
	rows, e := importexport.ReadXLSX(f, deviceImportColumns[:5])
	if e != nil {
		fail(w, 400, "IMPORT_FILE", e.Error())
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		errorResponse(w, e)
		return
	}
	result := importexport.Result{Errors: []importexport.RowError{}}
	for _, row := range rows {
		v := row.Values
		d := Device{ProjectID: FlexID(id), Vendor: v["厂商"], Model: v["型号"], Serial: v["SN"], Start: v["维保开始"], End: v["维保结束"], Status: map[string]string{"维保中": "active", "已续保": "renewed", "已下架": "retired"}[v["状态"]], Owner: v["负责人"], ContractNo: contract, Notes: v["备注"]}
		if e = validateDevice(d); e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: e.Error()})
			continue
		}
		if _, e = tx.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,owner,contract_no,notes) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, d.Vendor, d.Model, d.Serial, d.Start, d.End, defaultStatus(d.Status), d.Owner, d.ContractNo, d.Notes); e != nil {
			result.Errors = append(result.Errors, importexport.RowError{Number: row.Number, Message: "导入失败：SN 可能在项目内重复"})
			continue
		}
		result.Imported++
	}
	if e = tx.Commit(); e != nil {
		errorResponse(w, e)
		return
	}
	ok(w, result)
}

// testPushBody carries the text of a test push. The preview that the user
// confirmed is sent as it is, so that what they saw is what arrives.
type testPushBody struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	// Format is the way the message is written: html, txt or markdown. It
	// decides the PushPlus template the message is sent with.
	Format string `json:"format"`
}

// sendTestPush sends a message with the token of the current account and
// records the attempt in the log.
func (s *Server) sendTestPush(ctx context.Context, token push.Token, title, content string, actor string, format string) (string, error) {
	defaults := s.pushDefaults(ctx)
	out := push.Message{
		Token:   token.Token,
		Title:   title,
		Content: content,
		// The format of the message wins over the template of the token, so
		// that a message written in markdown is not sent as html.
		Template:    firstNonEmpty(format, token.Template, defaults["template"], "html"),
		Channel:     firstNonEmpty(token.Channel, defaults["channel"], "wechat"),
		Option:      token.Option,
		CallbackURL: firstNonEmpty(token.CallbackURL, defaults["callback_url"]),
	}
	if out.Token == "" {
		out.Token = s.cfg.PushPlusToken
	}
	if token.Kind == push.KindGroup {
		out.Topic = token.TopicCode
	}
	res, err := push.NewClientWithTimeout(timeoutSeconds(defaults["timeout_seconds"])).Send(ctx, out)
	if err != nil {
		s.log().Record(systemlog.Entry{
			Level:    systemlog.LevelError,
			Category: systemlog.CategoryPush,
			Action:   "test_push",
			Message:  "测试推送失败：" + err.Error(),
			Detail:   title + "｜" + content,
			Actor:    actor,
			IP:       clientIP(ctx),
		})
		return "", err
	}
	s.log().Record(systemlog.Entry{
		Level:    systemlog.LevelInfo,
		Category: systemlog.CategoryPush,
		Action:   "test_push",
		Message:  "测试推送已发送：" + title,
		Detail:   content,
		Actor:    actor,
		RefType:  "push_token",
		RefID:    token.ID,
		IP:       clientIP(ctx),
	})
	return res.MsgID, nil
}

// clientIP reads the address of the caller from the request context.
func clientIP(ctx context.Context) string {
	if v, ok := ctx.Value(remoteAddrKey).(string); ok {
		return v
	}
	return ""
}

func (s *Server) testDevicePush(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var body testPushBody
	// The body is optional: without it the built-in test message is used.
	_ = decode(r, &body)
	var p, c, v, m, sn, end, owner string
	e = s.db.QueryRow(`SELECT p.name,p.customer,d.vendor,d.model,d.serial_number,d.maintenance_end,COALESCE(NULLIF(d.owner,''),p.owner) FROM devices d JOIN projects p ON p.id=d.project_id WHERE d.id=?`, id).Scan(&p, &c, &v, &m, &sn, &end, &owner)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "设备不存在")
		return
	}
	store := push.NewStore(s.db)
	token, e2 := store.ForUser(currentUser(r).Username)
	if e2 != nil || !token.Enabled {
		token = push.Token{}
	}
	if token.ID == 0 {
		// The account has no token of its own, so the fallback token is
		// used, the same way the strategy test does.
		if t, e3 := store.Default(); e3 == nil {
			token = t
		}
	}
	// The text the user confirmed wins over the built-in one.
	title := firstNonEmpty(body.Title, "维保提醒测试")
	content := firstNonEmpty(body.Content, fmt.Sprintf("测试推送｜项目：%s｜客户：%s｜设备：%s %s｜SN：%s｜到期：%s｜负责人：%s", p, c, v, m, sn, end, owner))

	// The message must already be rendered. The dialog renders it through
	// the preview before it sends it, so a marker that is still here means
	// the message was not rendered and would reach the phone as "{{project}}".
	if keys := unresolvedMarkers(title, content); len(keys) > 0 {
		fail(w, 400, "VALIDATION", "消息中还留有未替换的占位符："+strings.Join(keys, "、"))
		return
	}
	msgID, e := s.sendTestPush(r.Context(), token, title, content, currentUser(r).Username, body.Format)
	if e != nil {
		fail(w, 502, "PUSH_FAILED", e.Error())
		return
	}
	ok(w, map[string]any{"sent": true, "msg_id": msgID})
}
func (s *Server) testStrategyPush(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var body testPushBody
	_ = decode(r, &body)
	var name, msg, recipients string
	var tokenID int64
	var format string
	e = s.db.QueryRow(`SELECT name,template,recipients,COALESCE(token_id,0),format FROM strategies WHERE id=?`, id).Scan(&name, &msg, &recipients, &tokenID, &format)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "策略不存在")
		return
	}
	targets := []string{}
	_ = json.Unmarshal([]byte(recipients), &targets)
	store := push.NewStore(s.db)
	token := push.Token{}
	if len(targets) > 0 {
		if t, e2 := store.ForUser(targets[0]); e2 == nil && t.Enabled {
			token = t
		}
	}
	if token.ID == 0 && tokenID > 0 {
		if t, e2 := store.Get(tokenID); e2 == nil && t.Enabled {
			token = t
		}
	}
	if token.ID == 0 {
		if t, e2 := store.Default(); e2 == nil {
			token = t
		}
	}
	title := firstNonEmpty(body.Title, "策略测试："+name)
	content := firstNonEmpty(body.Content, msg)

	// The message must already be rendered. The dialog renders it through
	// the preview before it sends it, so a marker that is still here means
	// the message was not rendered and would reach the phone as "{{project}}".
	if keys := unresolvedMarkers(title, content); len(keys) > 0 {
		fail(w, 400, "VALIDATION", "消息中还留有未替换的占位符："+strings.Join(keys, "、"))
		return
	}
	msgID, e := s.sendTestPush(r.Context(), token, title, content, currentUser(r).Username, firstNonEmpty(body.Format, format))
	if e != nil {
		fail(w, 502, "PUSH_FAILED", e.Error())
		return
	}
	s.log().Record(systemlog.Entry{
		Level:    systemlog.LevelInfo,
		Category: systemlog.CategoryStrategy,
		Action:   "strategy_test_push",
		Message:  "策略测试推送已发送：" + name,
		Detail:   content,
		Actor:    currentUser(r).Username,
		RefType:  "strategy",
		RefID:    id,
		IP:       clientIP(r.Context()),
	})
	ok(w, map[string]any{"sent": true, "msg_id": msgID})
}

// testDraftStrategyPush sends the message of a strategy that is being
// edited, before it is saved. The text is the one the user confirmed in the
// editor, so that the test shows what the saved strategy would send.
func (s *Server) testDraftStrategyPush(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title      string `json:"title"`
		Content    string `json:"content"`
		Name       string `json:"name"`
		TokenID    int64  `json:"token_id"`
		Recipients string `json:"recipients"`
		// Format is the way the message is written: html, txt or markdown.
		Format string `json:"format"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		fail(w, 400, "VALIDATION", "消息内容不能为空")
		return
	}
	store := push.NewStore(s.db)
	token := push.Token{}
	targets := []string{}
	_ = json.Unmarshal([]byte(body.Recipients), &targets)
	if len(targets) > 0 {
		if t, e := store.ForUser(targets[0]); e == nil && t.Enabled {
			token = t
		}
	}
	if token.ID == 0 && body.TokenID > 0 {
		if t, e := store.Get(body.TokenID); e == nil && t.Enabled {
			token = t
		}
	}
	if token.ID == 0 {
		if t, e := store.Default(); e == nil {
			token = t
		}
	}
	title := firstNonEmpty(body.Title, "策略测试")

	// The message must already be rendered. The editor renders it through
	// the preview before it sends it, so a marker that is still here means
	// the message was not rendered and would reach the phone as "{{project}}".
	if keys := unresolvedMarkers(title, body.Content); len(keys) > 0 {
		fail(w, 400, "VALIDATION", "消息中还留有未替换的占位符："+strings.Join(keys, "、"))
		return
	}
	msgID, e := s.sendTestPush(r.Context(), token, title, body.Content, currentUser(r).Username, body.Format)
	if e != nil {
		fail(w, 502, "PUSH_FAILED", e.Error())
		return
	}
	s.log().Record(systemlog.Entry{
		Level:    systemlog.LevelInfo,
		Category: systemlog.CategoryStrategy,
		Action:   "strategy_draft_test_push",
		Message:  "策略草稿测试推送已发送：" + firstNonEmpty(body.Name, "未命名策略"),
		Detail:   title + "｜" + body.Content,
		Actor:    currentUser(r).Username,
		IP:       clientIP(r.Context()),
	})
	ok(w, map[string]any{"sent": true, "msg_id": msgID})
}
