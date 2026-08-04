package site6v

import (
	"io"
	"net/http"
	"net/http/cookiejar"
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
func (c *Client) Get(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", c.Base+"/")
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
