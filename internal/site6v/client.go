package site6v

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Client 是 6v520.com 的 HTTP 客户端，自动以 GBK 解码响应。
type Client struct {
	Base string
	HTTP *http.Client
}

// NewClient 创建客户端，base 为站点根 URL。
// 内置 cookie jar，用于 EmpireCMS 站内搜索的 lastsearchtime 频控 cookie。
func NewClient(base string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		Base: base,
		HTTP: &http.Client{Timeout: 20 * time.Second, Jar: jar},
	}
}

// Get 抓取 url 并以 GBK 解码返回 HTML 字符串。
// 兼容旧调用方；新代码请使用 GetCtx。
func (c *Client) Get(url string) (string, error) {
	return c.GetCtx(context.Background(), url)
}

// GetCtx 带 context 抓取 url 并以 GBK 解码返回 HTML 字符串。
// context 取消时会中止 HTTP 请求，避免 goroutine 泄漏。
func (c *Client) GetCtx(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	c.setCommonHeaders(req)
	return c.doDecodeGBK(req)
}

// PostFormCtx 带 context 提交 application/x-www-form-urlencoded 表单并以 GBK 解码返回响应。
// checkRedirect 可选：传入则覆盖 http.Client 的 CheckRedirect（站内搜索需禁用自动重定向）。
func (c *Client) PostFormCtx(ctx context.Context, url, formBody string, checkRedirect func(*http.Request, []*http.Request) error) (string, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(formBody))
	if err != nil {
		return "", nil, err
	}
	c.setCommonHeaders(req)
	req.Header.Set("Origin", c.Base)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=gb2312")

	client := c.HTTP
	if checkRedirect != nil {
		client = &http.Client{
			Timeout:       c.HTTP.Timeout,
			Jar:           c.HTTP.Jar,
			CheckRedirect: checkRedirect,
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	// 调用方若需要响应头（如 Location），负责关闭 resp.Body；
	// 否则我们在读取完 body 后关闭。
	body, err := io.ReadAll(transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		resp.Body.Close()
		return "", nil, err
	}
	return string(body), resp, nil
}

func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", c.Base+"/")
}

// doDecodeGBK 执行请求并以 GBK 解码响应 body，调用方已设置好 req。
func (c *Client) doDecodeGBK(req *http.Request) (string, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
