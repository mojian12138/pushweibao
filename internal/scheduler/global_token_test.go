package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"maintenance-reminder/internal/push"
)

func TestDeliverUsesConfiguredGlobalToken(t *testing.T) {
	s := newScheduler(t)
	if _, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role,active) VALUES('alice','x','admin',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES('pushplus_token','db-global-token')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO notification_outbox(dedupe_key,recipient,title,message,status,next_attempt) VALUES('global-token-test','alice','标题','正文','pending',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body push.Message
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		got <- body.Token
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"msgId":"test"}}`))
	}))
	defer server.Close()
	s.Push = &push.Client{HTTP: server.Client(), Endpoint: server.URL}
	if err := s.Deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	if token := <-got; token != "db-global-token" {
		t.Fatalf("sent token=%q", token)
	}
}
