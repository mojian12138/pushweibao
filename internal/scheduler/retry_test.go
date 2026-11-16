package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"maintenance-reminder/internal/push"
)

// queueOutbox inserts one pending message and returns its id.
func queueOutbox(t *testing.T, s *Scheduler, attempts int) int64 {
	t.Helper()
	res, err := s.DB.Exec(`INSERT INTO notification_outbox(dedupe_key,message,status,attempts,next_attempt) VALUES(?,?,'failed',?,CURRENT_TIMESTAMP)`,
		"key-"+strconv.Itoa(attempts)+"-"+strconv.FormatInt(time.Now().UnixNano(), 10), "内容", attempts)
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestDeliverStopsAtTheRetryLimit(t *testing.T) {
	s := newScheduler(t)
	// The limit is low so the test does not have to wait for the backoff.
	if _, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES('push.retry_limit','2') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatalf("set retry limit: %v", err)
	}

	// The endpoint always fails, so every attempt is recorded.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":999,"msg":"服务端验证错误","data":"测试失败"}`))
	}))
	defer server.Close()
	s.Push = &push.Client{HTTP: server.Client(), Endpoint: server.URL}

	id := queueOutbox(t, s, 2)
	if err := s.Deliver(context.Background()); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	var status, lastError string
	var attempts int
	if err := s.DB.QueryRow(`SELECT status,attempts,last_error FROM notification_outbox WHERE id=?`, id).Scan(&status, &attempts, &lastError); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected the message to be left alone at 2 attempts, got %d", attempts)
	}
	if lastError == "" {
		t.Error("expected the retry limit to be recorded as the error")
	}
}

func TestDeliverRecordsThePushPlusCode(t *testing.T) {
	s := newScheduler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":999,"msg":"服务端验证错误","data":"过时的消息，请更新消息时间戳"}`))
	}))
	defer server.Close()
	s.Push = &push.Client{HTTP: server.Client(), Endpoint: server.URL}

	// A token has to exist, otherwise the message never reaches the server.
	store := push.NewStore(s.DB)
	tokenID, err := store.Create(push.Input{Name: "测试令牌", Kind: push.KindUser, Token: "test-token", Enabled: true})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	id := queueOutbox(t, s, 0)
	if _, err = s.DB.Exec(`UPDATE notification_outbox SET token_id=? WHERE id=?`, tokenID, id); err != nil {
		t.Fatalf("bind token: %v", err)
	}
	if err := s.Deliver(context.Background()); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	var code, detail string
	var succeeded int
	if err := s.DB.QueryRow(`SELECT succeeded,detail,code FROM notification_attempts WHERE outbox_id=?`, id).Scan(&succeeded, &detail, &code); err != nil {
		t.Fatalf("read attempt: %v", err)
	}
	if succeeded != 0 {
		t.Error("expected the attempt to be recorded as failed")
	}
	if code != "999" {
		t.Errorf("expected the code 999 to be recorded, got %q", code)
	}
	// The reason from the server has to survive, otherwise the operator only
	// sees a generic message.
	if detail == "" || !contains(detail, "过时的消息") {
		t.Errorf("expected the server reason to be recorded, got %q", detail)
	}
}

func TestTimeoutAndRetryLimitDefaults(t *testing.T) {
	if got := timeoutSeconds(""); got != 15*time.Second {
		t.Errorf("expected the default timeout of 15s, got %s", got)
	}
	if got := timeoutSeconds("abc"); got != 15*time.Second {
		t.Errorf("expected the default timeout of 15s for a bad value, got %s", got)
	}
	if got := timeoutSeconds("30"); got != 30*time.Second {
		t.Errorf("expected a 30s timeout, got %s", got)
	}
	if got := retryLimitOf(""); got != 6 {
		t.Errorf("expected the default retry limit of 6, got %d", got)
	}
	if got := retryLimitOf("0"); got != 6 {
		t.Errorf("expected the default retry limit of 6 for a bad value, got %d", got)
	}
	if got := retryLimitOf("3"); got != 3 {
		t.Errorf("expected a retry limit of 3, got %d", got)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
