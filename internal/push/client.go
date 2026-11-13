package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const Endpoint = "https://www.pushplus.plus/send"

type Client struct {
	HTTP     *http.Client
	Endpoint string
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Endpoint: Endpoint}
}
func (c *Client) Send(ctx context.Context, token, title, content string) error {
	if token == "" {
		return fmt.Errorf("PushPlus token 未配置")
	}
	body, _ := json.Marshal(map[string]string{"token": token, "title": title, "content": content, "template": "html"})
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
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PushPlus HTTP status %d", resp.StatusCode)
	}
	var result struct {
		Code    any    `json:"code"`
		Message string `json:"msg"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if fmt.Sprint(result.Code) != "200" {
		return fmt.Errorf("PushPlus: %s", result.Message)
	}
	return nil
}
