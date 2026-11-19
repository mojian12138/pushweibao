package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"maintenance-reminder/internal/systemlog"

	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/template"
)

// testMarker returns a short random marker used to make test messages
// unique, so that PushPlus does not refuse a repeated message as spam.
func testMarker() string {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// timeoutSeconds reads the configured request timeout, falling back to the
// documented default when the value is missing or not a number.
func timeoutSeconds(raw string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		n = int(push.DefaultTimeout / time.Second)
	}
	return time.Duration(n) * time.Second
}

// pushTokenView is the API shape of a token. The secret is never returned;
// only a masked form is exposed.
type pushTokenView struct {
	push.Token
	References map[string]int `json:"references"`
}

func (s *Server) listPushTokens(w http.ResponseWriter, r *http.Request) {
	store := push.NewStore(s.db)
	tokens, err := store.List()
	if err != nil {
		errorResponse(w, err)
		return
	}
	out := []pushTokenView{}
	for _, t := range tokens {
		users, strategies, outbox, e := store.References(t.ID)
		if e != nil {
			errorResponse(w, e)
			return
		}
		out = append(out, pushTokenView{Token: t, References: map[string]int{"users": users, "strategies": strategies, "outbox": outbox}})
	}
	ok(w, out)
}

func (s *Server) createPushToken(w http.ResponseWriter, r *http.Request) {
	var in push.Input
	if decode(r, &in) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if err := in.Normalize(); err != nil {
		fail(w, 400, "VALIDATION", err.Error())
		return
	}
	if in.Token == "" {
		fail(w, 400, "VALIDATION", "令牌值必填")
		return
	}
	id, err := push.NewStore(s.db).Create(in)
	if err != nil {
		errorResponse(w, err)
		return
	}
	token, err := push.NewStore(s.db).Get(id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	s.log().Record(systemlog.Entry{Level: systemlog.LevelInfo, Category: systemlog.CategorySystem, Action: "push_token_created", Message: "推送令牌已创建：" + token.Name, Detail: fmt.Sprintf("类型=%s 渠道=%s 启用=%t", token.Kind, token.Channel, token.Enabled), Actor: currentUser(r).Username, RefType: "push_token", RefID: id, IP: clientIP(r.Context())})
	writeJSON(w, 201, envelope{OK: true, Data: token})
}

func (s *Server) updatePushToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var in push.Input
	if decode(r, &in) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if err = in.Normalize(); err != nil {
		fail(w, 400, "VALIDATION", err.Error())
		return
	}
	store := push.NewStore(s.db)
	if err = store.Update(id, in); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "NOT_FOUND", "令牌不存在")
			return
		}
		errorResponse(w, err)
		return
	}
	token, err := store.Get(id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	s.log().Record(systemlog.Entry{Level: systemlog.LevelInfo, Category: systemlog.CategorySystem, Action: "push_token_updated", Message: "推送令牌已更新：" + token.Name, Detail: fmt.Sprintf("类型=%s 渠道=%s 启用=%t", token.Kind, token.Channel, token.Enabled), Actor: currentUser(r).Username, RefType: "push_token", RefID: id, IP: clientIP(r.Context())})
	ok(w, token)
}

func (s *Server) deletePushToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	if err = push.NewStore(s.db).Delete(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "NOT_FOUND", "令牌不存在")
			return
		}
		fail(w, 409, "IN_USE", err.Error())
		return
	}
	s.log().Record(systemlog.Entry{Level: systemlog.LevelInfo, Category: systemlog.CategorySystem, Action: "push_token_deleted", Message: fmt.Sprintf("推送令牌已删除 #%d", id), Actor: currentUser(r).Username, RefType: "push_token", RefID: id, IP: clientIP(r.Context())})
	ok(w, map[string]bool{"deleted": true})
}

// setPushTokenEnabled turns a token on or off without touching the rest.
func (s *Server) setPushTokenEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if err = push.NewStore(s.db).SetEnabled(id, req.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "NOT_FOUND", "令牌不存在")
			return
		}
		errorResponse(w, err)
		return
	}
	s.log().Record(systemlog.Entry{Level: systemlog.LevelInfo, Category: systemlog.CategorySystem, Action: "push_token_enabled_changed", Message: fmt.Sprintf("推送令牌 #%d 启用状态=%t", id, req.Enabled), Actor: currentUser(r).Username, RefType: "push_token", RefID: id, IP: clientIP(r.Context())})
	ok(w, map[string]bool{"enabled": req.Enabled})
}

// testPushToken sends a test message with the stored token settings.
func (s *Server) testPushToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, "BAD_REQUEST", "编号无效")
		return
	}
	store := push.NewStore(s.db)
	token, err := store.Get(id)
	if err != nil {
		fail(w, 404, "NOT_FOUND", "令牌不存在")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		// Format is the way the message is written: html, txt or markdown.
		Format string `json:"format"`
	}
	if decode(r, &req) != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		// PushPlus refuses repeated identical content, so the default message
		// carries a short marker that makes every attempt unique.
		req.Content = fmt.Sprintf("这是一条维保提醒系统的测试消息，收到即表示令牌配置正确。（测试 #%s）", testMarker())
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "维保提醒测试推送"
	}

	// The message must already be rendered. The dialog renders it through
	// the preview before it sends it, so a marker that is still here means
	// the message was not rendered and would reach the phone as "{{project}}".
	if keys := unresolvedMarkers(req.Title, req.Content); len(keys) > 0 {
		fail(w, 400, "VALIDATION", "消息中还留有未替换的占位符："+strings.Join(keys, "、"))
		return
	}

	defaults := s.pushDefaults(r.Context())
	globalToken := s.globalToken(r.Context())
	msg := push.Message{
		Token:       token.Token,
		Title:       req.Title,
		Content:     req.Content,
		Template:    firstNonEmpty(req.Format, token.Template, defaults["template"], "html"),
		Channel:     firstNonEmpty(token.Channel, defaults["channel"], "wechat"),
		Option:      firstNonEmpty(token.Option, defaults["option"]),
		CallbackURL: firstNonEmpty(token.CallbackURL, defaults["callback_url"]),
	}
	if token.Kind == push.KindGroup {
		msg.Topic = token.TopicCode
	}
	if msg.Token == "" {
		msg.Token = globalToken
	}
	res, err := push.NewClientWithTimeout(timeoutSeconds(defaults["timeout_seconds"])).Send(r.Context(), msg)
	if err != nil {
		s.log().Record(systemlog.Entry{
			Level:    systemlog.LevelError,
			Category: systemlog.CategoryPush,
			Action:   "token_test_push",
			Message:  "令牌测试推送失败：" + err.Error(),
			Detail:   req.Title + "｜" + req.Content,
			Actor:    currentUser(r).Username,
			RefType:  "push_token",
			RefID:    token.ID,
			IP:       clientIP(r.Context()),
		})
		fail(w, 502, "PUSH_FAILED", err.Error())
		return
	}
	s.log().Record(systemlog.Entry{
		Level:    systemlog.LevelInfo,
		Category: systemlog.CategoryPush,
		Action:   "token_test_push",
		Message:  "令牌测试推送已发送：" + token.Name,
		Detail:   req.Title + "｜" + req.Content,
		Actor:    currentUser(r).Username,
		RefType:  "push_token",
		RefID:    token.ID,
		IP:       clientIP(r.Context()),
	})
	ok(w, map[string]any{"sent": true, "msg_id": res.MsgID})
}

// pushOptions lists the templates and channels supported by PushPlus so the
// UI can render the option lists without hardcoding them.
func (s *Server) pushOptions(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"templates": push.Templates, "channels": push.Channels})
}

// messageFormats lists the message formats the editor offers, together with
// the ready-made templates of each one, so that the UI does not have to
// hardcode them.
//
// The three formats are the ones PushPlus supports for a message body:
// html, txt and markdown.
func (s *Server) messageFormats(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, f := range template.Formats {
		names := template.FormatTemplateNames(f)
		labels := []map[string]string{}
		for _, n := range names {
			labels = append(labels, map[string]string{"value": n, "label": template.PresetLabel(n)})
		}
		out = append(out, map[string]any{
			"value":   f,
			"label":   template.FormatLabel(f),
			"hint":    template.FormatHint(f),
			"presets": template.Presets(f),
			"names":   names,
			"labels":  labels,
		})
	}
	ok(w, out)
}
