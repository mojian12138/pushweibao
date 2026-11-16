package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"maintenance-reminder/internal/push"
	"maintenance-reminder/internal/template"
)

// TestEnqueueKeepsTheFormatOfTheStrategy checks that the format of a
// strategy travels with the queued message, so that a later edit of the
// strategy does not change a message that is already waiting.
func TestEnqueueKeepsTheFormatOfTheStrategy(t *testing.T) {
	s := newScheduler(t)
	tg := target{Recipient: "alice", Token: push.Token{ID: 0, Name: "令牌", Token: "tok"}}
	if err := s.enqueue(context.Background(), "key-1", device{ID: 0, Project: "项目A"}, "策略", "30 天", tg, "标题", "正文", template.FormatMarkdown); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var format string
	if err := s.DB.QueryRow(`SELECT format FROM notification_outbox WHERE dedupe_key='key-1'`).Scan(&format); err != nil {
		t.Fatalf("read the queued message: %v", err)
	}
	if format != template.FormatMarkdown {
		t.Errorf("expected the format to be kept, got %q", format)
	}
}

// TestDeliverUsesTheFormatOfTheQueuedMessage checks that a queued message
// is sent with the template of its own format, even when the token says
// something else.
func TestDeliverUsesTheFormatOfTheQueuedMessage(t *testing.T) {
	s := newScheduler(t)
	store := push.NewStore(s.DB)
	token, err := store.Create(push.Input{Name: "令牌", Kind: push.KindUser, Token: "tok", Enabled: true, Template: template.FormatHTML})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password,active,push_token_id) VALUES('alice','x','admin',0,1,?)`, token); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO projects(name,customer,contact,owner,notes,contract_no) VALUES('项目A','客户','','','','')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO devices(project_id,vendor,model,serial_number,maintenance_start,maintenance_end,status,owner,contract_no,notes) VALUES(1,'厂商','型号','SN-1','2026-01-01','2026-12-31','active','','','')`); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO notification_outbox(dedupe_key,device_id,project_name,strategy_name,trigger_point,recipient,title,message,channel,token_id,token_name,channel_detail,format) VALUES('key-1',1,'项目A','策略','30 天','alice','标题','正文','wechat',?,'令牌','',?)`, token, template.FormatMarkdown); err != nil {
		t.Fatalf("insert the queued message: %v", err)
	}

	type sent struct {
		Template string `json:"template"`
		Content  string `json:"content"`
	}
	got := make(chan sent, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body sent
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"msgId":"1"}}`))
	}))
	defer fake.Close()
	s.Push = &push.Client{HTTP: fake.Client(), Endpoint: fake.URL}

	if err := s.Deliver(context.Background()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	body := <-got
	if body.Template != template.FormatMarkdown {
		t.Errorf("expected the markdown template, got %q", body.Template)
	}
	if body.Content != "正文" {
		t.Errorf("expected the message to be sent as it is, got %q", body.Content)
	}
}
