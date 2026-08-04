package drive

import "testing"

func TestParseTitle(t *testing.T) {
	cases := []struct{ in, name, year string }{
		{"2026科幻惊悚《灵魂伴侣》1080p.HD中英双字", "灵魂伴侣", "2026"},
		{"2026爱情喜剧《不能错过的只有你2》4K.HD国语中字", "不能错过的只有你2", "2026"},
		{"《漂流》", "漂流", ""},
		{"没有书名号的标题", "没有书名号的标题", ""},
	}
	for _, c := range cases {
		n, y := parseTitle(c.in)
		if n != c.name || y != c.year {
			t.Errorf("parseTitle(%q) = (%q,%q), want (%q,%q)", c.in, n, y, c.name, c.year)
		}
	}
}

func TestParseSeason(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"S01E05", 1},
		{"S02", 2},
		{"Show.Name.S03E10.1080p", 3},
		{"第二季", 2},
		{"第十一季", 11},
		{"第二十三季", 23},
		{"第十季", 10},
		{"Season 3", 3},
		{"第1部", 1},
		{"无季信息", 1},
	}
	for _, c := range cases {
		got := ParseSeason(c.in)
		if got != c.want {
			t.Errorf("ParseSeason(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
