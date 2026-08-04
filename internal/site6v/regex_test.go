package site6v

import "testing"

// TestRegexInit 验证包 init 时所有正则都能编译。
// Go RE2 不支持反向引用（\1），曾导致 hrefRe panic，容器无法启动。
func TestRegexInit(t *testing.T) {
	if blueRe == nil || hrefRe == nil || detailPathRe == nil ||
		nonDetailRe == nil || detailFallbackRe == nil || itemRe == nil {
		t.Fatal("regex nil")
	}
}

func TestHrefRe(t *testing.T) {
	cases := []struct{ in, want string }{
		{`href="/dy/abc.html"`, "/dy/abc.html"},
		{`href='/dy/abc.html'`, "/dy/abc.html"},
		{`href=/dy/abc.html`, "/dy/abc.html"},
	}
	for _, c := range cases {
		m := hrefRe.FindStringSubmatch(c.in)
		got := ""
		for _, v := range m[1:4] {
			if v != "" {
				got = v
				break
			}
		}
		if got != c.want {
			t.Errorf("hrefRe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSearchItems(t *testing.T) {
	html := `<span class="blue14"><a href="/dy/2024/abc.html">测试电影</a></span>`
	items := parseSearchItems(html, "https://www.6v520.com")
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "测试电影" {
		t.Errorf("title = %q", items[0].Title)
	}
	if items[0].URL != "https://www.6v520.com/dy/2024/abc.html" {
		t.Errorf("url = %q", items[0].URL)
	}
	if items[0].Category != "dy" {
		t.Errorf("category = %q", items[0].Category)
	}
}
