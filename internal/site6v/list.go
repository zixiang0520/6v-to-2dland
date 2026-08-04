package site6v

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// categories 是 6v520.com 的全部资源分类目录名（列表页爬取 fallback 用）。
var categories = []string{
	"dy", "gydy", "gq", "zydy", "jddy", "3D",
	"dlz", "rj", "mj", "zy", "shoujidianyingmp4",
}

var (
	// 列表页条目（fallback 用）：<li><span>日期</span><a href="/分类/.../编号.html">标题</a>
	itemRe = regexp.MustCompile(`<li>\s*<span>(\d{4}-\d{2}-\d{2})</span>\s*<a href="(/[^"]+\.html)"[^>]*>([^<]+)</a>`)

	// 站内搜索结果条目：<span class="blue14"><a href="详情URL">标题</a></span>
	blueRe = regexp.MustCompile(`<span\s+class=["']?blue14["']?\s*>\s*<a\s+([^>]*?)>([\s\S]*?)</a>\s*</span>`)
	hrefRe = regexp.MustCompile(`href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

	// 详情页路径白名单：/分类/.../编号.html
	detailPathRe = regexp.MustCompile(`/(?:dy|gydy|gq|zydy|jddy|3D|dlz|rj|mj|zy|shoujidianyingmp4|juji|dsj|dm|dongman|dianshiju|zongyi|xiju|dongzuo|kehuan|aiqing|kongbu|zhanzheng|juqing|anime|lianzai|dianshi)/[A-Za-z0-9._/%-]+\.html`)
	nonDetailRe  = regexp.MustCompile(`/(?:sousuo|index|search|e/search)\b`)
	// 未列全的栏目兜底：/xx/yy.html
	detailFallbackRe = regexp.MustCompile(`/[a-z][a-z0-9]*/[A-Za-z0-9._/%-]+\.html`)

	tagRe = regexp.MustCompile(`<[^>]+>`)
)

var (
	searchMu     sync.Mutex
	lastSearchAt time.Time
)

// minSearchInterval 是站内搜索的最小间隔（EmpireCMS 有 lastsearchtime 频控）。
const minSearchInterval = 3 * time.Second

// Search 先用 EmpireCMS 站内搜索接口（1 个 POST 请求），失败再回退到列表页爬取。
// 旧实现爬 11 分类 × maxPages 页 = 88 个串行请求，站内搜索可降至 1~2 个请求。
func (c *Client) Search(ctx context.Context, keyword string, maxPages int) []Resource {
	kw := strings.TrimSpace(keyword)
	if rs := c.searchByAPI(ctx, kw); len(rs) > 0 {
		return rs
	}
	return c.searchByList(ctx, strings.ToLower(kw), maxPages)
}

// searchByAPI 调用 /e/search/index.php 站内搜索。
// 关键：keyboard 必须按 GBK 编码，UTF-8 编码会被站点判为"没有搜索到相关的内容"。
func (c *Client) searchByAPI(ctx context.Context, keyword string) []Resource {
	if keyword == "" {
		return nil
	}
	// 频控：站点要求搜索间隔 ≥3s，否则返回"请不要连续提交"
	searchMu.Lock()
	if !lastSearchAt.IsZero() {
		if wait := minSearchInterval - time.Since(lastSearchAt); wait > 0 {
			searchMu.Unlock()
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-t.C:
			case <-ctx.Done():
				return nil
			}
			searchMu.Lock()
		}
	}
	lastSearchAt = time.Now()
	searchMu.Unlock()

	htmlText, err := c.postSearch(ctx, keyword)
	if err != nil || htmlText == "" {
		return nil
	}
	// 站点反馈：无结果 / 频控
	if strings.Contains(htmlText, "没有搜索到相关的内容") || strings.Contains(htmlText, "请不要连续提交") || strings.Contains(htmlText, "访问过于频繁") {
		return nil
	}
	return parseSearchItems(htmlText, c.Base)
}

// postSearch POST 表单到站内搜索接口。
// EmpireCMS 流程：POST /e/search/index.php → 302 到 result/?searchid=xxx → GET result 页。
// 注意：不能让 http.Client 自动跟随 302，因为 GET 请求无 Content-Length 会触发 WAF 411。
// 这里禁用重定向，手动解析 Location 后用 GetCtx 取结果页。
func (c *Client) postSearch(ctx context.Context, keyword string) (string, error) {
	kwEnc, err := encodeGbkUri(keyword)
	if err != nil {
		return "", err
	}
	body := "show=title,smalltext&tempid=1&tbname=article&keyboard=" + kwEnc

	htmlText, resp, err := c.PostFormCtx(ctx, c.Base+"/e/search/index.php", body, func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	})
	if err != nil {
		return "", err
	}

	// 302/301: 手动跟随到 result 页
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if loc == "" {
			return "", nil
		}
		resultURL, err := resolveURL(c.Base+"/e/search/index.php", loc)
		if err != nil {
			return "", nil
		}
		return c.GetCtx(ctx, resultURL)
	}

	// 非 302：直接返回已解码的响应（可能是错误页/频控提示页）
	resp.Body.Close()
	return htmlText, nil
}

// resolveURL 把相对路径 ref 基于 base 解析为绝对 URL。
func resolveURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// encodeGbkUri 把字符串按 GBK 编码后做百分号编码。
// EmpireCMS 站内搜索要求 keyboard 为 GBK 编码（UTF-8 编码的中文会搜索失败）。
func encodeGbkUri(s string) (string, error) {
	enc := simplifiedchinese.GBK.NewEncoder()
	b, err := enc.Bytes([]byte(s))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, by := range b {
		switch {
		case (by >= '0' && by <= '9') || (by >= 'A' && by <= 'Z') || (by >= 'a' && by <= 'z'),
			by == '-' || by == '.' || by == '_' || by == '~':
			sb.WriteByte(by)
		default:
			fmt.Fprintf(&sb, "%%%02X", by)
		}
	}
	return sb.String(), nil
}

// parseSearchItems 从站内搜索结果 HTML 解析条目。
func parseSearchItems(htmlText, base string) []Resource {
	var items []Resource
	seen := make(map[string]bool)
	for _, m := range blueRe.FindAllStringSubmatch(htmlText, -1) {
		attrs, titleRaw := m[1], m[2]
		title := strings.Join(strings.Fields(tagRe.ReplaceAllString(html.UnescapeString(titleRaw), " ")), " ")
		hm := hrefRe.FindStringSubmatch(attrs)
		if hm == nil || title == "" {
			continue
		}
		href := ""
		for _, v := range hm[1:4] {
			if v != "" {
				href = v
				break
			}
		}
		if href == "" {
			continue
		}
		href = html.UnescapeString(strings.TrimSpace(href))
		if !isDetailPath(href) {
			continue
		}
		abs := absolutize(href, base)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		items = append(items, Resource{
			Title:    title,
			URL:      abs,
			Category: categoryFromPath(href),
		})
	}
	return items
}

func isDetailPath(href string) bool {
	if nonDetailRe.MatchString(href) {
		return false
	}
	if detailPathRe.MatchString(href) {
		return true
	}
	return detailFallbackRe.MatchString(href)
}

func categoryFromPath(href string) string {
	s := strings.TrimPrefix(href, "/")
	if i := strings.IndexByte(s, '/'); i > 0 {
		return s[:i]
	}
	return s
}

func absolutize(href, base string) string {
	switch {
	case strings.HasPrefix(href, "http://"), strings.HasPrefix(href, "https://"):
		return href
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/"):
		return base + href
	default:
		return base + "/" + href
	}
}

// searchByList 是 fallback：并发爬取各分类列表页，按关键词模糊匹配标题。
// 仅在站内搜索接口失败/无结果时使用。
func (c *Client) searchByList(ctx context.Context, kwLower string, maxPages int) []Resource {
	var mu sync.Mutex
	var results []Resource
	var wg sync.WaitGroup
	for _, cat := range categories {
		wg.Add(1)
		go func(cat string) {
			defer wg.Done()
			rs := c.searchCategory(ctx, cat, kwLower, maxPages)
			if len(rs) == 0 {
				return
			}
			mu.Lock()
			results = append(results, rs...)
			mu.Unlock()
		}(cat)
	}
	wg.Wait()

	seen := make(map[string]bool, len(results))
	uniq := make([]Resource, 0, len(results))
	for _, r := range results {
		if seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		uniq = append(uniq, r)
	}
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Date > uniq[j].Date })
	return uniq
}

func (c *Client) searchCategory(ctx context.Context, cat, kw string, maxPages int) []Resource {
	var results []Resource
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return results
		default:
		}
		var u string
		if page == 1 {
			u = c.Base + "/" + cat + "/"
		} else {
			u = c.Base + "/" + cat + "/index_" + strconv.Itoa(page) + ".html"
		}
		htmlText, err := c.GetCtx(ctx, u)
		if err != nil {
			break
		}
		matches := itemRe.FindAllStringSubmatch(htmlText, -1)
		if len(matches) == 0 {
			break
		}
		for _, m := range matches {
			date, href, title := m[1], m[2], m[3]
			if kw == "" || strings.Contains(strings.ToLower(title), kw) {
				results = append(results, Resource{
					Title:    strings.TrimSpace(title),
					URL:      c.Base + href,
					Date:     date,
					Category: cat,
				})
			}
		}
	}
	return results
}
