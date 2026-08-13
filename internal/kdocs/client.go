package kdocs

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	baiduURLPattern = regexp.MustCompile(`https?://pan\.baidu\.com/(?:s/[A-Za-z0-9_-]+|share/init\?surl=[A-Za-z0-9_-]+)(?:[?&][A-Za-z0-9_%=.-]+)*`)
	pwdPattern      = regexp.MustCompile(`(?i)(?:提取码|密码|pwd)\s*[:：]?\s*([A-Za-z0-9]{4})`)
)

type Resource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Pwd   string `json:"pwd"`
}

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return errors.New("KDocs URL 必须是有效的 HTTPS 地址")
	}
	host := strings.ToLower(u.Hostname())
	if host != "kdocs.cn" && !strings.HasSuffix(host, ".kdocs.cn") {
		return errors.New("KDocs URL 必须属于 kdocs.cn")
	}
	return nil
}

func (c *Client) Fetch(ctx context.Context, pageURL string) ([]Resource, string, error) {
	if err := ValidateURL(pageURL); err != nil {
		return nil, "", err
	}

	doc, err := c.fetchHTML(ctx, pageURL)
	if err == nil {
		if resources := ExtractResources(doc); len(resources) > 0 {
			return resources, "http", nil
		}
	}

	doc, browserErr := renderWithBrowser(ctx, pageURL)
	if browserErr != nil {
		if err != nil {
			return nil, "", fmt.Errorf("直接抓取失败（%v），浏览器渲染也失败：%w", err, browserErr)
		}
		return nil, "", fmt.Errorf("公开页面未在 HTML 中提供资源，且浏览器渲染失败：%w", browserErr)
	}
	resources := ExtractResources(doc)
	if len(resources) == 0 {
		return nil, "", errors.New("页面已渲染，但未发现百度网盘链接；金山文档可能将正文绘制在 Canvas 中，当前最简抓取方案无法读取")
	}
	return resources, "browser", nil
}

func (c *Client) fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/138.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("KDocs 返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func renderWithBrowser(ctx context.Context, pageURL string) (string, error) {
	browser, err := findBrowser()
	if err != nil {
		return "", err
	}
	profile, err := os.MkdirTemp("", "kdocs-browser-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(profile)

	args := []string{
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--no-first-run",
		"--disable-extensions",
		"--disable-background-networking",
		"--user-data-dir=" + profile,
		"--virtual-time-budget=20000",
		"--dump-dom",
		pageURL,
	}
	cmd := exec.CommandContext(ctx, browser, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("启动无头浏览器失败（可通过 KDOCS_BROWSER 指定 Chrome/Edge）：%w", err)
	}
	if len(out) == 0 {
		return "", errors.New("无头浏览器没有返回 DOM（可通过 KDOCS_BROWSER 指定 Chrome/Edge）")
	}
	return string(out), nil
}

func findBrowser() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("KDOCS_BROWSER")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("KDOCS_BROWSER 不可用：%w", err)
		}
		return configured, nil
	}

	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
		for _, path := range candidates {
			if path != "" {
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
	}
	return "", errors.New("未找到 Chrome/Chromium/Edge；请安装浏览器或设置 KDOCS_BROWSER")
}

func ExtractResources(document string) []Resource {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(document))
	if err != nil {
		return nil
	}

	var blocks []string
	doc.Find("h1,h2,h3,h4,h5,h6,p,li,td,blockquote,.otl-paragraph,a").Each(func(_ int, selection *goquery.Selection) {
		text := cleanText(selection.Text())
		for _, attr := range []string{"url", "href"} {
			if link, ok := selection.Attr(attr); ok && strings.Contains(link, "pan.baidu.com/") && !strings.Contains(text, link) {
				text += " " + link
			}
		}
		if text != "" {
			blocks = append(blocks, text)
		}
	})
	if len(blocks) == 0 {
		blocks = strings.Split(cleanText(doc.Text()), "\n")
	}

	seen := make(map[string]bool)
	resources := make([]Resource, 0)
	for i, block := range blocks {
		urls := baiduURLPattern.FindAllString(html.UnescapeString(block), -1)
		for _, rawURL := range urls {
			rawURL = strings.TrimRight(rawURL, ".,，。;；)]）")
			if seen[rawURL] {
				continue
			}
			seen[rawURL] = true
			contextText := block
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				contextText = blocks[j] + " " + contextText
			}
			for j := i + 1; j < len(blocks) && j <= i+2; j++ {
				contextText += " " + blocks[j]
			}
			resources = append(resources, Resource{
				Title: findTitle(blocks, i, rawURL),
				URL:   rawURL,
				Pwd:   findPwd(contextText, rawURL),
			})
		}
	}
	return resources
}

func findTitle(blocks []string, index int, rawURL string) string {
	current := cleanTitle(strings.ReplaceAll(blocks[index], rawURL, ""))
	if current != "" && !genericTitle(current) {
		return current
	}
	for i := index - 1; i >= 0 && i >= index-4; i-- {
		candidate := cleanTitle(blocks[i])
		if candidate != "" && !genericTitle(candidate) && !baiduURLPattern.MatchString(candidate) && pwdPattern.FindString(candidate) == "" {
			return candidate
		}
	}
	return "未命名资源"
}

func genericTitle(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "下载", "下载地址", "链接", "网盘链接", "百度网盘", "百度云", "点击下载":
		return true
	default:
		return false
	}
}

func cleanTitle(value string) string {
	value = pwdPattern.ReplaceAllString(value, "")
	value = strings.TrimSpace(strings.Trim(value, "-—|丨:：;；,，。()（）[]【】"))
	if len([]rune(value)) > 160 {
		return ""
	}
	return value
}

func findPwd(value, rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if pwd := u.Query().Get("pwd"); pwd != "" {
			return pwd
		}
	}
	if match := pwdPattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1]
	}
	return ""
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\u00a0", " ")), " ")
}
