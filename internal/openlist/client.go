package openlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client 是 OpenList/AList (v3 API) 的最小客户端：登录 + 目录列表。
type Client struct {
	Base string
	User string
	Pass string

	mu          sync.Mutex
	token       string
	tokenExpire time.Time
	http        *http.Client
}

// New 创建客户端。base 形如 http://nas.zx.al:5244。
func New(base, user, pass string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		User: user,
		Pass: pass,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// Configured 判断是否已填写 OpenList 地址。
func (c *Client) Configured() bool { return c != nil && c.Base != "" }

// Entry 是目录列表里的一项。
type Entry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// post 调用 OpenList API 并解析统一响应结构。
func (c *Client) post(ctx context.Context, path string, body any, auth bool) (json.RawMessage, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		c.mu.Lock()
		tok := c.token
		c.mu.Unlock()
		if tok != "" {
			req.Header.Set("Authorization", tok)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("OpenList 响应解析失败: %w", err)
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("OpenList %s: %s", path, r.Message)
	}
	return r.Data, nil
}

// Token 获取（必要时重新登录）访问令牌。
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpire) {
		return c.token, nil
	}
	data, err := c.post(ctx, "/api/auth/login", map[string]string{
		"username": c.User,
		"password": c.Pass,
	}, false)
	if err != nil {
		return "", fmt.Errorf("OpenList 登录失败: %w", err)
	}
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &d); err != nil || d.Token == "" {
		return "", fmt.Errorf("OpenList 登录响应无 token")
	}
	c.token = d.Token
	c.tokenExpire = time.Now().Add(40 * time.Hour) // AList token 默认 48h，提前刷新
	return c.token, nil
}

// Ping 验证账号密码可用（登录即验证）。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Token(ctx)
	return err
}

// ListDir 列出目录内容。path 为 OpenList 上的绝对路径。
func (c *Client) ListDir(ctx context.Context, path string) ([]Entry, error) {
	if _, err := c.Token(ctx); err != nil {
		return nil, err
	}
	data, err := c.post(ctx, "/api/fs/list", map[string]any{
		"path":     path,
		"password": "",
		"page":     1,
		"per_page": 0, // 0 = 全部
		"refresh":  false,
	}, true)
	if err != nil {
		return nil, err
	}
	var d struct {
		Content []Entry `json:"content"`
		Total   int64   `json:"total"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("OpenList 目录响应解析失败: %w", err)
	}
	if d.Content == nil {
		return []Entry{}, nil // 空目录
	}
	return d.Content, nil
}

// Test 连通性测试：登录 + 列根目录。
func (c *Client) Test(ctx context.Context) error {
	if err := c.Ping(ctx); err != nil {
		return err
	}
	if _, err := c.ListDir(ctx, "/"); err != nil {
		return err
	}
	log.Printf("openlist: test ok (%s)", c.Base)
	return nil
}
