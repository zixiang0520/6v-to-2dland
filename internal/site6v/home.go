package site6v

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// categoryCN 把分类目录名映射为中文（与 drive.categoryNames 保持一致）。
var categoryCN = map[string]string{
	"dy": "电影", "gydy": "国语电影", "gq": "经典高清",
	"zydy": "动漫", "jddy": "动画电影", "3D": "3D电影",
	"dlz": "国剧", "rj": "日韩剧", "mj": "欧美剧",
	"zy": "综艺", "shoujidianyingmp4": "手机电影",
}

func categoryCNName(cat string) string {
	if n, ok := categoryCN[cat]; ok {
		return n
	}
	return cat
}

// FetchBrowse 并发抓取所有分类的列表页，每个分类取前 perCategory 条。
// 用于发现页：按分类浏览 6v520 的资源（列表页无封面图，前端用首字母占位）。
// 11 个分类并发爬取，单分类内串行翻页直到收够 perCategory 条或无更多页。
func (c *Client) FetchBrowse(ctx context.Context, perCategory int) ([]BrowseCategory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if perCategory <= 0 {
		perCategory = 100
	}

	type result struct {
		cat   string
		items []HomeItem
	}
	var wg sync.WaitGroup
	ch := make(chan result, len(categories))
	for _, cat := range categories {
		wg.Add(1)
		go func(cat string) {
			defer wg.Done()
			rs := c.fetchCategoryTopN(ctx, cat, perCategory)
			items := make([]HomeItem, 0, len(rs))
			for _, r := range rs {
				items = append(items, HomeItem{
					Title:    r.Title,
					URL:      r.URL,
					Category: r.Category,
					Date:     r.Date,
				})
			}
			ch <- result{cat, items}
		}(cat)
	}
	wg.Wait()
	close(ch)

	// 按 categories 原始顺序输出，跳过空分类
	byCat := make(map[string][]HomeItem, len(categories))
	for r := range ch {
		byCat[r.cat] = r.items
	}
	out := make([]BrowseCategory, 0, len(categories))
	for _, cat := range categories {
		items := byCat[cat]
		if len(items) == 0 {
			continue
		}
		out = append(out, BrowseCategory{
			Category: cat,
			Name:     categoryCNName(cat),
			Items:    items,
		})
	}
	return out, nil
}

// fetchCategoryTopN 爬取某分类列表页，直到收集 n 条或无更多页。
// 复用 list.go 的 itemRe 提取 <li><span>日期</span><a href="...">标题</a>。
func (c *Client) fetchCategoryTopN(ctx context.Context, cat string, n int) []Resource {
	var results []Resource
	seen := make(map[string]bool)
	for page := 1; len(results) < n; page++ {
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
		htmlText, err := c.Get(u)
		if err != nil {
			break
		}
		matches := itemRe.FindAllStringSubmatch(htmlText, -1)
		if len(matches) == 0 {
			break
		}
		added := 0
		for _, m := range matches {
			date, href, title := m[1], m[2], m[3]
			abs := c.Base + href
			if seen[abs] {
				continue
			}
			seen[abs] = true
			results = append(results, Resource{
				Title:    strings.TrimSpace(title),
				URL:      abs,
				Date:     date,
				Category: cat,
			})
			added++
			if len(results) >= n {
				break
			}
		}
		if added == 0 {
			break
		}
	}
	return results
}
