package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsDocumentedFields(t *testing.T) {
	var got Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"请求成功","data":"msg-123","traceId":"trace-1"}`))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), Endpoint: server.URL}
	res, err := client.Send(context.Background(), Message{Token: "abc", Title: "标题", Content: "内容"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.MsgID != "msg-123" {
		t.Errorf("expected msg id msg-123, got %q", res.MsgID)
	}
	if res.TraceID != "trace-1" {
		t.Errorf("expected trace id trace-1, got %q", res.TraceID)
	}
	if got.Token != "abc" || got.Title != "标题" || got.Content != "内容" {
		t.Errorf("unexpected payload: %+v", got)
	}
	if got.Template != "html" {
		t.Errorf("expected the html template default, got %q", got.Template)
	}
	if got.Channel != "wechat" {
		t.Errorf("expected the wechat channel default, got %q", got.Channel)
	}
}

func TestSendReportsBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":903,"msg":"token无效"}`))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), Endpoint: server.URL}
	_, err := client.Send(context.Background(), Message{Token: "bad"})
	if err == nil {
		t.Fatal("expected an error for a non-200 business code")
	}
	if !strings.Contains(err.Error(), "903") {
		t.Errorf("expected the error to mention the code, got %v", err)
	}
}

func TestSendOmitsAnEmptyTimestamp(t *testing.T) {
	// PushPlus reads a zero timestamp as the epoch and refuses the message as
	// stale, so the field must never reach the wire unless it is set.
	body, err := json.Marshal(Message{Token: "abc", Title: "标题", Content: "内容"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "timestamp") {
		t.Errorf("expected the timestamp to be left out of the payload, got %s", body)
	}
}

func TestSendKeepsAnExplicitTimestamp(t *testing.T) {
	body, err := json.Marshal(Message{Token: "abc", Timestamp: 1794550540000})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"timestamp":1794550540000`) {
		t.Errorf("expected the timestamp to be sent, got %s", body)
	}
}

func TestSendReportsTheServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":999,"msg":"服务端验证错误","data":"过时的消息，请更新消息时间戳"}`))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), Endpoint: server.URL}
	_, err := client.Send(context.Background(), Message{Token: "abc"})
	if err == nil {
		t.Fatal("expected an error for a non-200 business code")
	}
	// The generic message alone would leave the operator guessing.
	if !strings.Contains(err.Error(), "过时的消息") {
		t.Errorf("expected the server reason to be reported, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *APIError, got %T", err)
	}
	if apiErr.Code != 999 {
		t.Errorf("expected the code 999, got %d", apiErr.Code)
	}
}

func TestSendRequiresToken(t *testing.T) {
	client := NewClient()
	if _, err := client.Send(context.Background(), Message{}); err == nil {
		t.Fatal("expected an error when the token is empty")
	}
}

func TestExplainCoversDocumentedCodes(t *testing.T) {
	cases := map[int]string{
		200: "发送成功",
		302: "未登录",
		401: "请求未授权",
		403: "请求 IP 未授权",
		500: "系统异常",
		888: "积分不足",
		903: "无效的用户令牌",
		905: "实名认证",
	}
	for code, want := range cases {
		if got := Explain(code, ""); !strings.Contains(got, want) {
			t.Errorf("code %d: expected %q, got %q", code, want, got)
		}
	}
}

func TestMaskToken(t *testing.T) {
	if got := MaskToken(""); got != "" {
		t.Errorf("expected an empty mask for an empty token, got %q", got)
	}
	got := MaskToken("abcdefghijklmnop")
	if strings.Contains(got, "efghijkl") {
		t.Errorf("expected the middle of the token to be hidden, got %q", got)
	}
	if !strings.HasPrefix(got, "abcd") || !strings.HasSuffix(got, "mnop") {
		t.Errorf("expected the token edges to stay visible, got %q", got)
	}
}

func TestValidChannelAndTemplate(t *testing.T) {
	if !ValidChannel("sms") || !ValidChannel("") {
		t.Error("expected sms and an empty value to be valid channels")
	}
	if ValidChannel("telegram") {
		t.Error("expected telegram to be rejected")
	}
	if !ValidTemplate("markdown") || ValidTemplate("xml") {
		t.Error("unexpected template validation result")
	}
}

// TestChannelCodesAreLowercase guards the channel codes against the casing
// PushPlus expects: the documented codes are all lower case, and a mismatched
// one is refused by the server.
func TestChannelCodesAreLowercase(t *testing.T) {
	for _, c := range Channels {
		if c.Value != strings.ToLower(c.Value) {
			t.Errorf("channel %q must be lower case", c.Value)
		}
	}
	if !ValidChannel("webhook") {
		t.Error("expected the webhook channel to be valid")
	}
}
