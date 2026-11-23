package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New("key", "", "zh-CN")
	c.base = srv.URL
	return c
}

const multiResult = `{"results":[
 {"title":"同名电影 A","release_date":"2024-05-01"},
 {"title":"灵魂伴侣","release_date":"2026-02-14"},
 {"title":"The Soul Mate","original_title":"灵魂伴侣","release_date":"2019-01-01"}
]}`

func TestBestPrefersExactTitleAndYear(t *testing.T) {
	c := newTestClient(t, multiResult)
	r, err := c.Best(context.Background(), "灵魂伴侣", "movie", "2026")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Title != "灵魂伴侣" || r.Date != "2026-02-14" {
		t.Fatalf("got %+v, want 灵魂伴侣/2026-02-14", r)
	}
}

func TestBestRejectsNothingAlike(t *testing.T) {
	c := newTestClient(t, multiResult)
	r, err := c.Best(context.Background(), "完全不相干的纪录片", "movie", "2020")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatalf("expected no reliable match, got %+v", r)
	}
}

func TestBestYearMismatchStillMatchesByTitle(t *testing.T) {
	c := newTestClient(t, `{"results":[{"title":"老片","release_date":"1994-09-10"}]}`)
	r, err := c.Best(context.Background(), "老片", "movie", "2026")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Title != "老片" {
		t.Fatalf("title match should survive year mismatch, got %+v", r)
	}
}

func TestBigramSim(t *testing.T) {
	if s := bigramSim("灵魂伴侣", "灵魂伴侣"); s != 1 {
		t.Fatalf("identical = %v", s)
	}
	if s := bigramSim("灵魂伴侣", "灵魂羁绊"); s >= 0.7 {
		t.Fatalf("different titles too similar: %v", s)
	}
	if s := bigramSim("x", "y"); s != 0 {
		t.Fatalf("short = %v", s)
	}
}

func TestNormalizeTitleStripsSeason(t *testing.T) {
	if got := normalizeTitle("怪奇物语 第一季"); got != "怪奇物语" {
		t.Fatalf("got %q", got)
	}
}

// 别名场景：6v 名「守护星」在 TMDB 搜不到可靠结果（词条名是「守护你，遇见爱」），
// 配了别名后 Best 应按别名搜索并命中。
func TestBestUsesAlias(t *testing.T) {
	c := newTestClient(t, `{"results":[
	 {"title":"银河护卫队3","release_date":"2023-05-05"},
	 {"title":"守护你，遇见爱","original_title":"Guarding Stars","release_date":"2026-11-18"}
	]}`)
	c.SetAliases(map[string]string{"守护星": "守护你，遇见爱"})
	r, err := c.Best(context.Background(), "守护星", "movie", "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Title != "守护你，遇见爱" {
		t.Fatalf("alias should hit 守护你，遇见爱, got %+v", r)
	}
}

// 别名归一化：key 带书名号/空格/大小写差异也能命中。
func TestBestAliasNormalizedKeys(t *testing.T) {
	c := newTestClient(t, `{"results":[
	 {"title":"完美骗子","original_title":"Perfectos mentirosos","release_date":"2026-01-01"}
	]}`)
	c.SetAliases(map[string]string{"《完美说谎者》": "完美骗子"})
	r, err := c.Best(context.Background(), "完美说谎者", "movie", "2026")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Title != "完美骗子" {
		t.Fatalf("normalized alias should hit, got %+v", r)
	}
}

// 无别名时行为不变：低分候选照样拒绝。
func TestBestWithoutAliasStillRejects(t *testing.T) {
	c := newTestClient(t, `{"results":[
	 {"title":"守护你，遇见爱","release_date":"2026-11-18"}
	]}`)
	r, err := c.Best(context.Background(), "守护星", "movie", "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatalf("no alias configured, low-score candidate must be rejected, got %+v", r)
	}
}
