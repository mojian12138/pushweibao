package push

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveSend checks the real endpoint with the real client. It only runs
// when a token is provided in PUSHPLUS_LIVE_TOKEN.
func TestLiveSend(t *testing.T) {
	token := os.Getenv("PUSHPLUS_LIVE_TOKEN")
	if token == "" {
		t.Skip("no live token")
	}
	res, err := NewClient().Send(context.Background(), Message{
		Token:    token,
		Title:    "维保提醒系统回归测试",
		Content:  fmt.Sprintf("回归验证 #%d，收到即表示推送链路正常。", time.Now().UnixNano()),
		Template: "txt",
		Channel:  "wechat",
	})
	t.Logf("res=%+v err=%v", res, err)
	if err != nil {
		t.Errorf("live send failed: %v", err)
	}
}
