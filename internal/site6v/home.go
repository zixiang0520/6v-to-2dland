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

// gvodItemRe 匹配最新页：<li><span>[08-14]</span><a href="/dy/...html">标题</a>
var gvodItemRe = regexp.MustCompile(`<li>\s*<span>\[(\d{2}-\d{2})\]</span>\s*<a href="(/[^"]+\.html)"[^>]*>([\s\S]*?)</a>`)

func categoryCNName(cat string) string {
	if n, ok := categoryCN[cat]; ok {
		return n
	}
	return cat
}

// recentCutoff 返回「近 days 日」的最早日期（含当天）。
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

// parseGvodDate 把最新页的 [MM-DD] 补成年月日。跨年时：日期晚于今天则算上一年。
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

// FetchRecent 发现页按栏返回：
//  1. /gvod/zx.html、/gvod/dsj.html 各一栏，整页全抓（不过滤日期）
//  2. 11 个分类各一栏，只收近 days 天
//
// 栏内按发布日期降序；栏与栏之间不去重（同一部可同时出现在「最新」和分类里）。
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

	type keyed struct {
		id    string
		name  string
		items []HomeItem
	}
	n := len(gvodSources) + len(categories)
	var wg sync.WaitGroup
	ch := make(chan keyed, n)

	for _, src := range gvodSources {
		wg.Add(1)
		go func(path, name string) {
			defer wg.Done()
			id := "gvod-zx"
			if strings.Contains(path, "dsj") {
				id = "gvod-dsj"
			}
			items := c.fetchGvodPage(ctx, path, name, now)
			sortItemsByDate(items)
			ch <- keyed{id: id, name: name, items: items}
		}(src.Path, src.Name)
	}
	for _, cat := range categories {
		wg.Add(1)
		go func(cat string) {
			defer wg.Done()
			items := c.fetchCategoryRecent(ctx, cat, cutoff)
			sortItemsByDate(items)
			ch <- keyed{id: cat, name: categoryCNName(cat), items: items}
		}(cat)
	}
	wg.Wait()
	close(ch)

	got := make(map[string]keyed, n)
	for k := range ch {
		got[k.id] = k
	}

	order := make([]string, 0, n)
	for _, src := range gvodSources {
		if strings.Contains(src.Path, "dsj") {
			order = append(order, "gvod-dsj")
		} else {
			order = append(order, "gvod-zx")
		}
	}
	order = append(order, categories...)

	out := make([]HomeCategory, 0, n)
	for _, id := range order {
		k, ok := got[id]
		if !ok {
			continue
		}
		if k.items == nil {
			k.items = []HomeItem{}
		}
		out = append(out, HomeCategory{Category: k.id, Name: k.name, Items: k.items})
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

func (c *Client) fetchGvodPage(ctx context.Context, path, sourceName string, now time.Time) []HomeItem {
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
	return out
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
