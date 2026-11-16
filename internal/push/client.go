package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Endpoint is the PushPlus send endpoint. It is a variable so that tests
// can point the client at a local server.
var Endpoint = "https://www.pushplus.plus/send"

// Templates supported by PushPlus.
var Templates = []string{"html", "txt", "json", "markdown", "cloudMonitor", "jenkins", "route", "pay", "form", "doc", "excel"}

// Channels supported by PushPlus. Paid channels are flagged so the UI can
// warn before a message consumes credits.
var Channels = []Channel{
	{Value: "wechat", Label: "微信服务号", Free: true},
	{Value: "app", Label: "App（安卓/鸿蒙/iOS）", Free: true},
	{Value: "extension", Label: "插件（浏览器扩展/桌面应用）", Free: true},
	{Value: "webhook", Label: "Webhook（企业微信、钉钉、飞书机器人）", Free: true},
	{Value: "clawbot", Label: "微信 ClawBot", Free: true},
	{Value: "cmcc", Label: "新消息 ClawBot（移动，短信形式）", Free: true},
	{Value: "qq", Label: "QQ 机器人", Free: true},
	{Value: "cp", Label: "企业微信应用", Free: true},
	{Value: "mail", Label: "邮件", Free: true},
	{Value: "sms", Label: "短信（收费，1 条扣 10 积分）", Free: false},
	{Value: "voice", Label: "语音（收费，1 条呼叫扣 30 积分）", Free: false},
}

type Channel struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Free  bool   `json:"free"`
}

// Message carries every parameter the PushPlus send endpoint accepts.
type Message struct {
	Token       string `json:"token"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Template    string `json:"template"`
	Topic       string `json:"topic"`
	Channel     string `json:"channel"`
	Option      string `json:"option"`
	CallbackURL string `json:"callbackUrl"`
	// Timestamp schedules the message. It must be a millisecond epoch that is
	// later than the current time: PushPlus rejects a timestamp that is in the
	// past. It is omitted from the request when zero, because a zero value
	// would be read as the epoch and every message would be refused as stale.
	Timestamp int64 `json:"timestamp,omitempty"`
}

// Result carries the identifiers returned by PushPlus on success.
type Result struct {
	MsgID   string `json:"msg_id"`
	TraceID string `json:"trace_id"`
	Code    int    `json:"code"`
}

// APIError is a business error reported by PushPlus. The code is kept so the
// caller can record it and act on the well known ones.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("PushPlus %d：%s", e.Code, e.Message)
}

type Client struct {
	HTTP     *http.Client
	Endpoint string
}

// DefaultTimeout is the request timeout used when none is configured.
const DefaultTimeout = 15 * time.Second

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: DefaultTimeout}, Endpoint: Endpoint}
}

// NewClientWithTimeout builds a client that gives up after the given
// duration. A non-positive duration falls back to the default.
func NewClientWithTimeout(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{HTTP: &http.Client{Timeout: timeout}, Endpoint: Endpoint}
}

// Send posts a message and returns the PushPlus identifiers. A non-200
// business code is translated into a readable Chinese message.
func (c *Client) Send(ctx context.Context, msg Message) (Result, error) {
	msg = normalize(msg)
	if msg.Token == "" {
		return Result{}, fmt.Errorf("PushPlus token 未配置")
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return Result{}, err
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("PushPlus HTTP 状态 %d", resp.StatusCode)
	}
	var result struct {
		Code    any    `json:"code"`
		Message string `json:"msg"`
		Data    any    `json:"data"`
		TraceID string `json:"traceId"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, err
	}
	code := codeOf(result.Code)
	if code != 200 {
		// The server puts the real reason in "data" while "msg" stays generic,
		// so both are reported to make the failure actionable.
		reason := Explain(code, result.Message)
		if detail := dataOf(result.Data); detail != "" {
			reason = reason + "：" + detail
		}
		return Result{Code: code}, &APIError{Code: code, Message: reason}
	}
	return Result{MsgID: dataOf(result.Data), TraceID: result.TraceID, Code: code}, nil
}

// codeOf normalises the business code, which PushPlus returns either as a
// number or as a numeric string.
func codeOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func dataOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	default:
		return ""
	}
}

// NowTimestamp returns the millisecond epoch to use for a scheduled message.
// PushPlus refuses a timestamp that is not in the future, so callers that
// want to schedule should look a little ahead of this value.
func NowTimestamp() int64 { return time.Now().UnixMilli() }

// Explain maps PushPlus return codes to actionable messages.
func Explain(code int, fallback string) string {
	switch code {
	case 200:
		return "发送成功"
	case 302:
		return "未登录"
	case 401:
		return "请求未授权，请检查 token 是否正确"
	case 403:
		return "请求 IP 未授权，请在开放接口中将本机 IP 加入白名单"
	case 500:
		return "系统异常，请稍后再试"
	case 600:
		return "数据异常，操作失败"
	case 805:
		return "无权查看"
	case 888:
		return "积分不足，需要充值"
	case 900:
		return "账号使用受限（请求次数过多），请稍后再试"
	case 903:
		return "无效的用户令牌，请检查 token 是否正确"
	case 905:
		return "账户未进行实名认证，实名认证后即可发送消息"
	case 999:
		return "服务端验证错误（常见原因：消息时间戳已过期、参数不合法或推送过于频繁）"
	default:
		if fallback == "" {
			return "未知错误"
		}
		return fallback
	}
}

// normalize applies the documented defaults and drops empty parameters.
func normalize(msg Message) Message {
	if msg.Template == "" {
		msg.Template = "html"
	}
	if msg.Channel == "" {
		msg.Channel = "wechat"
	}
	msg.Template = strings.ToLower(strings.TrimSpace(msg.Template))
	msg.Channel = strings.TrimSpace(msg.Channel)
	msg.Topic = strings.TrimSpace(msg.Topic)
	msg.Option = strings.TrimSpace(msg.Option)
	msg.CallbackURL = strings.TrimSpace(msg.CallbackURL)
	return msg
}

// ValidTemplate reports whether the template is one of the documented values.
func ValidTemplate(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return true
	}
	for _, x := range Templates {
		if x == t {
			return true
		}
	}
	return false
}

// ValidChannel reports whether the channel is one of the documented values.
func ValidChannel(c string) bool {
	c = strings.TrimSpace(c)
	if c == "" {
		return true
	}
	for _, x := range Channels {
		if x.Value == c {
			return true
		}
	}
	return false
}

// MaskToken hides the middle of a token, mirroring how PushPlus shows it.
func MaskToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		if token == "" {
			return ""
		}
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", 8) + token[len(token)-4:]
}
