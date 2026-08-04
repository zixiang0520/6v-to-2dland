package site6v

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// categories 是 6v520.com 的全部资源分类目录名。
var categories = []string{
	"dy", "gydy", "gq", "zydy", "jddy", "3D",
	"dlz", "rj", "mj", "zy", "shoujidianyingmp4",
}

// itemRe 匹配列表页条目：<li><span>日期</span><a href="/分类/.../编号.html">标题</a>
var itemRe = regexp.MustCompile(`<li>\s*<span>(\d{4}-\d{2}-\d{2})</span>\s*<a href="(/[^"]+\.html)"[^>]*>([^<]+)</a>`)

// Search 在全部分类的列表页中按关键词模糊匹配标题，返回命中资源。
// maxPages 限制每个分类的翻页深度。
func (c *Client) Search(ctx context.Context, keyword string, maxPages int) []Resource {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	var mu sync.Mutex
	var results []Resource
	var wg sync.WaitGroup
	for _, cat := range categories {
		wg.Add(1)
		go func(cat string) {
			defer wg.Done()
			rs := c.searchCategory(ctx, cat, kw, maxPages)
			if len(rs) == 0 {
				return
			}
			mu.Lock()
			results = append(results, rs...)
			mu.Unlock()
		}(cat)
	}
	wg.Wait()

	// 去重（同一 URL 可能出现在多个分类）
	seen := make(map[string]bool, len(results))
	uniq := make([]Resource, 0, len(results))
	for _, r := range results {
		if seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		uniq = append(uniq, r)
	}
	// 按日期倒序
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Date > uniq[j].Date })
	return uniq
}

// searchCategory 翻页爬取单个分类，返回标题包含 kw 的条目。
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
		html, err := c.Get(u)
		if err != nil {
			break // 网络错误或翻页越界，停止该分类
		}
		matches := itemRe.FindAllStringSubmatch(html, -1)
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
