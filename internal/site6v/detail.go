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
