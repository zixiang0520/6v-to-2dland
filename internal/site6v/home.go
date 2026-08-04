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

// FetchBrowse 抓取分类的列表页，每个分类取前 perCategory 条。
// 用于发现页：按分类浏览 6v520 的资源（列表页无封面图，前端用文字列表展示）。
//   - cat 为空：并发爬取全部 11 个分类，perCategory 默认 100。
//   - cat 非空：仅爬取该分类（用户点击分类标签后切换展示），perCategory 默认 100。
//
// 单分类内串行翻页直到收够 perCategory 条或无更多页。
func (c *Client) FetchBrowse(ctx context.Context, perCategory int, cat string) ([]BrowseCategory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	// 默认每分类前 100 条（取消 20/100 两档加载，直接一次拉满）
	if perCategory <= 0 {
		perCategory = 100
	}

	// 选择要爬取的分类列表
	cats := categories
	if cat != "" {
		cats = []string{cat}
	}

	type result struct {
		cat   string
		items []HomeItem
	}
	var wg sync.WaitGroup
	ch := make(chan result, len(cats))
	for _, c2 := range cats {
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
		}(c2)
	}
	wg.Wait()
	close(ch)

	// 按 categories 原始顺序输出，跳过空分类
	byCat := make(map[string][]HomeItem, len(cats))
	for r := range ch {
		byCat[r.cat] = r.items
	}
	out := make([]BrowseCategory, 0, len(cats))
	for _, c2 := range categories {
		items := byCat[c2]
		if len(items) == 0 {
			continue
		}
		out = append(out, BrowseCategory{
			Category: c2,
			Name:     categoryCNName(c2),
			Items:    items,
		})
	}
	// 单分类请求时直接按抓取结果输出（避免 cat 不在 categories 时返回空）
	if cat != "" && len(out) == 0 {
		if items := byCat[cat]; len(items) > 0 {
			out = append(out, BrowseCategory{
				Category: cat,
				Name:     categoryCNName(cat),
				Items:    items,
			})
		}
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
		htmlText, err := c.GetCtx(ctx, u)
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
