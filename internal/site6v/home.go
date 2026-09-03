package site6v

import (
	"context"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CategoryRecentDays 11 个分类列表只保留近 N 个自然日（含今天）。
const CategoryRecentDays = 10

// recentMaxPages 单分类最多翻页，防止日期乱序或站点异常时无限爬。
const recentMaxPages = 20

// homepageSource 站点首页：最新数据（比 gvod 页面更新），整页全抓不过滤日期。
var homepageSource = struct {
	Path string
	Name string
}{Path: "/", Name: "首页推荐"}

// gvodSources 主人指定的「最新」页：整页全抓，不过滤日期。
var gvodSources = []struct {
	Path string
	Name string
}{
	{"/gvod/zx.html", "最新电影"},
	{"/gvod/dsj.html", "最新电视剧"},
}

// categoryCN 把分类目录名映射为中文（与 drive.categoryNames 保持一致）。
var categoryCN = map[string]string{
	"dy": "电影", "gydy": "国语电影", "gq": "经典高清",
	"zydy": "动漫", "jddy": "动画电影", "3D": "3D电影",
	"dlz": "国剧", "rj": "日韩剧", "mj": "欧美剧",
	"zy": "综艺", "shoujidianyingmp4": "手机电影",
}

// gvodItemRe 匹配最新页/首页：<li><span>[08-14]</span><a href="/dy/...html">标题</a>
// 首页的条目 HTML 结构与 gvod 页一致，复用同一正则。
var gvodItemRe = regexp.MustCompile(`<li>\s*<span>\[(\d{2}-\d{2})\]</span>\s*<a href="(/[^"]+\.html)"[^>]*>([\s\S]*?)</a>`)

func categoryCNName(cat string) string {
	if n, ok := categoryCN[cat]; ok {
		return n
	}
	return cat
}

func recentCutoff(now time.Time, days int) time.Time {
	if days <= 0 {
		days = CategoryRecentDays
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return today.AddDate(0, 0, -(days - 1))
}

func parseListDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseGvodDate 把最新页/首页的 [MM-DD] 补成年月日。跨年时：日期晚于今天则算上一年。
func parseGvodDate(mmdd string, now time.Time) (string, bool) {
	mmdd = strings.TrimSpace(mmdd)
	t, err := time.ParseInLocation("01-02", mmdd, now.Location())
	if err != nil {
		return "", false
	}
	y, _, _ := now.Date()
	got := time.Date(y, t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	today := time.Date(y, now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if got.After(today) {
		got = got.AddDate(-1, 0, 0)
	}
	return got.Format("2006-01-02"), true
}

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// fetchSourcePage 抓取一个「最新页」（首页或 gvod），返回排序后的条目。
func (c *Client) fetchSourcePage(ctx context.Context, path, sourceName string, now time.Time) []HomeItem {
	htmlText, err := c.GetCtx(ctx, c.Base+path)
	if err != nil || htmlText == "" {
		return nil
	}
	matches := gvodItemRe.FindAllStringSubmatch(htmlText, -1)
	out := make([]HomeItem, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		mmdd, href, title := m[1], m[2], stripTags(m[3])
		date, ok := parseGvodDate(mmdd, now)
		if !ok || title == "" {
			continue
		}
		abs := c.Base + href
		if seen[abs] {
			continue
		}
		seen[abs] = true
		cat := categoryFromPath(href)
		out = append(out, HomeItem{
			Title:        title,
			URL:          abs,
			Category:     cat,
			CategoryName: categoryCNName(cat),
			Date:         date,
			Source:       sourceName,
		})
	}
	sortItemsByDate(out)
	return out
}

// mergeIntoCats 把一批条目按 URL 路径合并进 catMap（按 URL 去重）。
func mergeIntoCats(catMap map[string][]HomeItem, items []HomeItem) {
	for _, it := range items {
		catMap[it.Category] = append(catMap[it.Category], it)
	}
}

// uniqueByURL 按 URL 去重（保留首次出现）。
func uniqueByURL(items []HomeItem) []HomeItem {
	seen := make(map[string]bool, len(items))
	out := make([]HomeItem, 0, len(items))
	for _, it := range items {
		if seen[it.URL] {
			continue
		}
		seen[it.URL] = true
		out = append(out, it)
	}
	return out
}

// FetchRecent 发现页按栏返回：
//  1. 首页（/）→ 「首页推荐」独立栏，放在最前
//  2. /gvod/zx.html、/gvod/dsj.html 各一栏（保留原有独立展示）
//  3. 11 个分类各一栏，近 days 天，已合并首页+gvod 条目
//
// 首页/gvod 条目会按 URL 路径分发到对应分类栏，与分类列表结果去重后合并。
// 栏内按发布日期降序。
func (c *Client) FetchRecent(ctx context.Context, days int) ([]HomeCategory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if days <= 0 {
		days = CategoryRecentDays
	}
	cutoff := recentCutoff(time.Now(), days)
	now := time.Now()

	type result struct {
		path  string
		items []HomeItem
	}
	type catRes struct {
		cat   string
		items []HomeItem
	}

	allSrcs := make([]struct {
		path string
		name string
	}, 0, 1+len(gvodSources))
	allSrcs = append(allSrcs, struct {
		path string
		name string
	}{homepageSource.Path, homepageSource.Name})
	for _, src := range gvodSources {
		allSrcs = append(allSrcs, struct {
			path string
			name string
		}{src.Path, src.Name})
	}

	var wg sync.WaitGroup
	srcCh := make(chan result, len(allSrcs))
	catCh := make(chan catRes, len(categories))

	for _, src := range allSrcs {
		wg.Add(1)
		go func(path, _ string) {
			defer wg.Done()
			items := c.fetchSourcePage(ctx, path, homepageSource.Name, now)
			srcCh <- result{path: path, items: items}
		}(src.path, src.name)
	}
	for _, cat := range categories {
		wg.Add(1)
		go func(cat string) {
			defer wg.Done()
			items := c.fetchCategoryRecent(ctx, cat, cutoff)
			catCh <- catRes{cat: cat, items: items}
		}(cat)
	}
	wg.Wait()
	close(srcCh)
	close(catCh)

	// 收集首页条目（用于「首页推荐」独立栏）
	highlightItems := make([]HomeItem, 0, 30)
	// 首页 + gvod 条目按 URL 路径合并到各分类栏
	catMap := make(map[string][]HomeItem, len(categories))

	for r := range srcCh {
		if r.path == homepageSource.Path {
			highlightItems = append(highlightItems, r.items...)
		}
		mergeIntoCats(catMap, r.items)
	}

	// 合并分类列表结果
	for r := range catCh {
		existing := catMap[r.cat]
		seen := make(map[string]bool, len(existing))
		for _, it := range existing {
			seen[it.URL] = true
		}
		for _, it := range r.items {
			if seen[it.URL] {
				continue
			}
			seen[it.URL] = true
			catMap[r.cat] = append(catMap[r.cat], it)
		}
	}

	// 组装输出
	out := make([]HomeCategory, 0, 1+len(gvodSources)+len(categories))

	// ① 首页推荐栏
	highlightItems = uniqueByURL(highlightItems)
	sortItemsByDate(highlightItems)
	out = append(out, HomeCategory{
		Category: "home",
		Name:     homepageSource.Name,
		Items:    highlightItems,
	})

	// ② gvod 独立栏（重新取，保持独立展示）
	gvodFetch := func(path, name string) []HomeItem {
		items := c.fetchSourcePage(ctx, path, name, now)
		if items == nil {
			return []HomeItem{}
		}
		return items
	}
	for _, src := range gvodSources {
		id := "gvod-zx"
		name := "最新电影"
		if strings.Contains(src.Path, "dsj") {
			id = "gvod-dsj"
			name = "最新电视剧"
		}
		items := gvodFetch(src.Path, name)
		sortItemsByDate(items)
		out = append(out, HomeCategory{Category: id, Name: name, Items: items})
	}

	// ③ 分类栏（已合并首页+gvod 条目）
	for _, cat := range categories {
		items := catMap[cat]
		items = uniqueByURL(items)
		sortItemsByDate(items)
		if items == nil {
			items = []HomeItem{}
		}
		out = append(out, HomeCategory{Category: cat, Name: categoryCNName(cat), Items: items})
	}

	return out, nil
}

func sortItemsByDate(items []HomeItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date > items[j].Date
		}
		return false
	})
}

// fetchCategoryRecent 爬某分类列表，收到早于 cutoff 的日期后停止翻页。
func (c *Client) fetchCategoryRecent(ctx context.Context, cat string, cutoff time.Time) []HomeItem {
	var results []HomeItem
	seen := make(map[string]bool)
	for page := 1; page <= recentMaxPages; page++ {
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
		hitOld := false
		added := 0
		for _, m := range matches {
			date, href, title := m[1], m[2], m[3]
			dt, ok := parseListDate(date)
			if !ok || dt.Before(cutoff) {
				hitOld = true
				continue
			}
			abs := c.Base + href
			if seen[abs] {
				continue
			}
			seen[abs] = true
			results = append(results, HomeItem{
				Title:        strings.TrimSpace(title),
				URL:          abs,
				Category:     cat,
				CategoryName: categoryCNName(cat),
				Date:         date,
			})
			added++
		}
		if hitOld || added == 0 {
			break
		}
	}
	return results
}