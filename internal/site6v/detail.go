package site6v

import (
	"context"
	"html"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FetchMagnets 抓取详情页，提取磁力链列表。
// 磁力链结构：<td bgcolor="#ffffbb" ...>磁力：<a href="magnet:?xt=urn:btih:...">描述</a>
func (c *Client) FetchMagnets(ctx context.Context, detailURL string) ([]Magnet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	page, err := c.GetCtx(ctx, detailURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return nil, err
	}
	var magnets []Magnet
	doc.Find(`td[bgcolor="#ffffbb"] a[href^="magnet:"]`).Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		// 属性值可能仍含 HTML 实体（&amp;），统一反转义
		magnet := html.UnescapeString(strings.TrimSpace(href))
		if magnet == "" {
			return
		}
		desc := strings.TrimSpace(s.Text())
		magnets = append(magnets, Magnet{
			Name:   desc,
			Magnet: magnet,
			Desc:   desc,
		})
	})
	return magnets, nil
}

// FetchTitle 抓取 6v 页面标题（关注链接名称用）。
// 优先取详情页正文标题（h1），否则取 <title> 并清洗掉站点后缀与
// “免费下载/迅雷下载/分类”等噪声尾巴；失败返回空串与错误。
func (c *Client) FetchTitle(ctx context.Context, pageURL string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	page, err := c.GetCtx(ctx, pageURL)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return "", err
	}
	// 1) 正文标题（详情页绿色大标题，如「《牧神记》更新101」）
	if h := strings.TrimSpace(html.UnescapeString(doc.Find("h1").First().Text())); h != "" && len([]rune(h)) >= 4 {
		return clean6vTitle(h), nil
	}
	// 2) <title> 兜底
	title := html.UnescapeString(strings.TrimSpace(doc.Find("title").First().Text()))
	// 去掉站点后缀与站名噪声
	for _, suf := range []string{"- 6v电影", "_6v电影", "-6v电影", "6v电影", "六v电影", "- 6v520", "_6v520", "6v520", "www.6v520.com"} {
		title = strings.ReplaceAll(title, suf, "")
	}
	title = strings.TrimSpace(strings.Trim(title, "-—_|｜·:： ,，"))
	return clean6vTitle(title), nil
}

// clean6vTitle 清理 6v 标题尾巴：「《xx》更新101，免费下载，迅雷下载，动漫」→「《xx》更新101」。
func clean6vTitle(t string) string {
	for _, sep := range []string{"，免费下载", "，迅雷下载", ",免费下载", ",迅雷下载", " 免费下载", " 迅雷下载", "，同步更新", "，在线观看"} {
		if i := strings.Index(t, sep); i > 0 {
			t = t[:i]
		}
	}
	return strings.TrimSpace(strings.Trim(t, "-—_|｜·:： ,，"))
}
