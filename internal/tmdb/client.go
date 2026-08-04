package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// Client 通过（可选）代理访问 TMDB API。
type Client struct {
	apiKey string
	lang   string
	base   string
	http   *http.Client
}

// New 创建 TMDB 客户端；proxy 为空则直连。
func New(apiKey, proxy, lang string) *Client {
	if lang == "" {
		lang = "zh-CN"
	}
	transport := &http.Transport{}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &Client{
		apiKey: apiKey,
		lang:   lang,
		base:   "https://api.themoviedb.org",
		http:   &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}
}

// Result 是一条搜索结果。
type Result struct {
	Title string
	Date  string // release_date 或 first_air_date
}

// Search 按名字搜索，mediaType 为 "movie" 或 "tv"；无结果返回 (nil, nil)。
func (c *Client) Search(ctx context.Context, query, mediaType string) (*Result, error) {
	if c.apiKey == "" {
		return nil, errors.New("tmdb api key 未配置")
	}
	u := fmt.Sprintf("%s/3/search/%s?api_key=%s&query=%s&language=%s",
		c.base, mediaType, c.apiKey, url.QueryEscape(query), c.lang)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Results []struct {
			Title        string `json:"title"`
			Name         string `json:"name"`
			ReleaseDate  string `json:"release_date"`
			FirstAirDate string `json:"first_air_date"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Results) == 0 {
		return nil, nil
	}
	r := data.Results[0]
	title := r.Title
	if title == "" {
		title = r.Name
	}
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAirDate
	}
	return &Result{Title: title, Date: date}, nil
}

var yearRe = regexp.MustCompile(`^(\d{4})`)

// YearFromDate 从 YYYY-MM-DD 提取年份。
func YearFromDate(s string) string {
	if m := yearRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
