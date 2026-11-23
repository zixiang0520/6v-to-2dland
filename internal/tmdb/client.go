package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Client 通过（可选）代理访问 TMDB API。
type Client struct {
	apiKey string
	lang   string
	base   string
	http   *http.Client

	mu      sync.RWMutex
	aliases map[string]string // 归一化 6v 标题 -> TMDB 实际标题（本地别名表）
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
		apiKey:  apiKey,
		lang:    lang,
		base:    "https://api.themoviedb.org",
		http:    &http.Client{Transport: transport, Timeout: 15 * time.Second},
		aliases: map[string]string{},
	}
}

// SetAliases 设置本地别名表：key/value 都是原始写法，内部归一化后比较。
// 用途：6v 的中文片名与 TMDB 词条名不一致时（译名差异、简称、暂无中词条目），
// 手工维护一份「6v 名 → TMDB 名」映射，Best 命中别名后按 TMDB 名搜索。
func (c *Client) SetAliases(m map[string]string) {
	if c == nil {
		return
	}
	norm := make(map[string]string, len(m))
	for k, v := range m {
		kk, vv := normalizeTitle(k), normalizeTitle(v)
		if kk == "" || vv == "" || kk == vv {
			continue
		}
		norm[kk] = vv
	}
	c.mu.Lock()
	c.aliases = norm
	c.mu.Unlock()
}

// lookupAlias 返回 query 对应的别名；无别名返回空串。
func (c *Client) lookupAlias(query string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.aliases) == 0 {
		return ""
	}
	return c.aliases[normalizeTitle(query)]
}

// NewWithBase 创建指向自定义 base 的客户端（测试或自建镜像用）。
func NewWithBase(apiKey, proxy, lang, base string) *Client {
	c := New(apiKey, proxy, lang)
	if base != "" {
		c.base = strings.TrimRight(base, "/")
	}
	return c
}

// Result 是一条搜索结果。
type Result struct {
	Title         string
	OriginalTitle string
	Date          string // release_date 或 first_air_date
}

type searchResponse struct {
	Results []struct {
		Title         string  `json:"title"`
		Name          string  `json:"name"`
		OriginalTitle string  `json:"original_title"`
		OriginalName  string  `json:"original_name"`
		ReleaseDate   string  `json:"release_date"`
		FirstAirDate  string  `json:"first_air_date"`
		Popularity    float64 `json:"popularity"`
	} `json:"results"`
}

// SearchAll 拉取搜索结果全部候选（默认每页 20 条）。
func (c *Client) SearchAll(ctx context.Context, query, mediaType string) ([]Result, error) {
	if c.apiKey == "" {
		return nil, errors.New("tmdb api key 未配置")
	}
	u := fmt.Sprintf("%s/3/search/%s?api_key=%s&query=%s&language=%s&include_adult=false",
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
	var data searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	var all []Result
	for _, r := range data.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		orig := r.OriginalTitle
		if orig == "" {
			orig = r.OriginalName
		}
		date := r.ReleaseDate
		if date == "" {
			date = r.FirstAirDate
		}
		if title == "" {
			continue
		}
		all = append(all, Result{Title: title, OriginalTitle: orig, Date: date})
	}
	return all, nil
}

// Search 兼容旧调用：返回搜索结果的第一条（无结果返回 nil,nil）。
func (c *Client) Search(ctx context.Context, query, mediaType string) (*Result, error) {
	all, err := c.SearchAll(ctx, query, mediaType)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	return &all[0], nil
}

// matchThreshold 是 Best() 认定「匹配可靠」的最低得分。
// 低于阈值说明候选和查询对不上（重名/别名/无关片），宁可放弃规范化也不给错目录名。
// 别名命中后（QueryAlias 模式）阈值放宽为 aliasThreshold：查询本身已经是
// 人工确认过的正确片名，评分再苛刻只会漏配，40 分足以挡住完全无关的候选。
const (
	matchThreshold = 45
	aliasThreshold = 40
)

// Best 在全部候选里选与 query/year 最匹配的一条；无可靠匹配返回 (nil, nil)。
// 评分：标题完全相等/互为包含/字面相似度，再加年份对齐加减分。
// year 为空时只看标题；调用方在带年份匹配失败后应再以空年份重试一次
// （6v 标题里的年份常是发布年份，不一定是影片年份）。
// 查找顺序：① 本地别名表把 query 映射成 TMDB 名后再搜（评分阈值放宽）；
// ② 原始 query 直接搜（正常阈值）。别名表由设置页维护。
func (c *Client) Best(ctx context.Context, query, mediaType, year string) (*Result, error) {
	best := func(q string, threshold int) *Result {
		all, err := c.SearchAll(ctx, q, mediaType)
		if err != nil || len(all) == 0 {
			return nil
		}
		nq := normalizeTitle(q)
		if nq == "" {
			return nil
		}
		bestIdx, bestScore := -1, -1<<30
		for i := range all {
			s := scoreCandidate(nq, year, all[i])
			if s > bestScore {
				bestScore, bestIdx = s, i
			}
		}
		if bestIdx < 0 || bestScore < threshold {
			return nil
		}
		return &all[bestIdx]
	}
	// ① 别名优先：人工维护的映射可信度高于字面评分。
	if alt := c.lookupAlias(query); alt != "" {
		if r := best(alt, aliasThreshold); r != nil {
			return r, nil
		}
	}
	// ② 原始 query。
	return best(query, matchThreshold), nil
}

// scoreCandidate 给单条候选打分。
func scoreCandidate(q, year string, r Result) int {
	s := 0
	t := normalizeTitle(r.Title)
	o := normalizeTitle(r.OriginalTitle)
	switch {
	case t == q || (o != "" && o == q):
		s += 80
	case strings.Contains(t, q) || strings.Contains(q, t),
		o != "" && (strings.Contains(o, q) || strings.Contains(q, o)):
		s += 55
	default:
		sim := bigramSim(q, t)
		if o != "" {
			if s2 := bigramSim(q, o); s2 > sim {
				sim = s2
			}
		}
		switch {
		case sim >= 0.75:
			s += 50
		case sim >= 0.5:
			s += 30
		default:
			s -= 20
		}
	}
	if year != "" {
		y := YearFromDate(r.Date)
		if y == year {
			s += 40
		} else if y != "" {
			d := yearDist(y, year)
			if d == 1 {
				s += 15
			} else {
				s -= 35
			}
		}
	}
	return s
}

func yearDist(a, b string) int {
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	d := x - y
	if d < 0 {
		d = -d
	}
	return d
}

// normalizeTitle 归一化标题：小写、剔除空格/标点/符号、去掉常见季后缀。
// 只保留字母、数字和汉字，让「灵 魂 伴 侣」与「灵魂伴侣」可以相等。
func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	for _, suffix := range []string{"第1季", "第一季"} {
		out = strings.TrimSuffix(out, suffix)
	}
	return out
}

// bigramSim 计算两个字符串的字符 bigram Dice 相似度（0~1）。
func bigramSim(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		if a == b && a != "" {
			return 1
		}
		return 0
	}
	set := make(map[string]int, len(ra)-1)
	for i := 0; i+1 < len(ra); i++ {
		set[string(ra[i:i+2])]++
	}
	inter := 0
	for i := 0; i+1 < len(rb); i++ {
		k := string(rb[i : i+2])
		if set[k] > 0 {
			set[k]--
			inter++
		}
	}
	total := (len(ra) - 1) + (len(rb) - 1)
	if total == 0 {
		return 0
	}
	return 2 * float64(inter) / float64(total)
}

var yearRe = regexp.MustCompile(`^(\d{4})`)

// YearFromDate 从 YYYY-MM-DD 提取年份。
func YearFromDate(s string) string {
	if m := yearRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
