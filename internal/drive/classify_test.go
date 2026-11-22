package drive

import (
	"testing"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

func TestShouldTrashFileKeepsSubtitles(t *testing.T) {
	keep := []string{"灵魂伴侣.S01E05.1080p.chs.ass", "字幕.srt", "poster.jpg", "movie.nfo", "封面.png", "audio.mka", "字体.ttf"}
	for _, n := range keep {
		if shouldTrashFile(n) {
			t.Errorf("%q should be kept, not trashed", n)
		}
	}
	trash := []string{"更多资源.txt", "访问.html", "下载地址.url", "破解.exe", "种子.torrent", "setup.dll"}
	for _, n := range trash {
		if !shouldTrashFile(n) {
			t.Errorf("%q should be trashed", n)
		}
	}
}

func TestClassifyFiles(t *testing.T) {
	files := []*userfile.File{
		{Identity: "1", Name: "某剧.S01E01.mkv"},
		{Identity: "2", Name: "某剧.S01E01.chs.srt"},
		{Identity: "3", Name: "更多资源.txt"},
		{Identity: "4", Name: "poster.jpg"},
	}
	videos, junkIDs, junkNames := classifyFiles(files)
	if len(videos) != 1 || videos[0].Name != "某剧.S01E01.mkv" {
		t.Fatalf("videos = %v", videos)
	}
	if len(junkIDs) != 1 || junkIDs[0] != "3" || junkNames[0] != "更多资源.txt" {
		t.Fatalf("junk = %v / %v", junkIDs, junkNames)
	}
}

func TestParseEpisodeBareNumbers(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"某剧.01.1080p.mkv", 1},
		{"某剧 - 12 - 1080p.mkv", 12},
		{"某剧 [07].mkv", 7},
		{"某剧.第3期.mp4", 3},
		{"某剧.2024.1080p.mkv", 0},   // 年份不是集数
		{"某剧.1080p.WEB-DL.mkv", 0}, // 分辨率不是集数
		{"X战警97.1080p.mkv", 0},     // 片名带数字不算
		{"第01-02集合集.mkv", 1},       // epCnRe 命中 01
	}
	for _, c := range cases {
		if got := parseEpisode(c.in); got != c.want {
			t.Errorf("parseEpisode(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestUniqueVideoName(t *testing.T) {
	used := map[string]bool{}
	if got := uniqueVideoName(used, "某剧S01E05.mkv", "某剧.S01E05.2160p.mkv"); got != "某剧S01E05.mkv" {
		t.Fatalf("first = %q", got)
	}
	// 第二个版本：撞名 → 带分辨率标识
	got := uniqueVideoName(used, "某剧S01E05.mkv", "某剧.S01E05.1080p.mkv")
	if got != "某剧S01E05.1080p.mkv" {
		t.Fatalf("second = %q, want 分辨率后缀", got)
	}
	// 第三个版本：无分辨率 → 序号
	got = uniqueVideoName(used, "某剧S01E05.mkv", "某剧.S01E05.mkv")
	if got != "某剧S01E05-2.mkv" {
		t.Fatalf("third = %q", got)
	}
}

func TestParseTitleNumericNames(t *testing.T) {
	cases := []struct {
		in   string
		name string
		year string
	}{
		{"[电影天堂www.dy2018.com]1921.2021.1080p.HD国语中字", "1921", "2021"},
		{"2012.2009.1080p.BluRay", "2012", "2009"},
		{"某站 2025 最新大片 纯净版", "某站 最新大片", "2025"},
		{"2026科幻惊悚《灵魂伴侣》1080p.HD中英双字", "灵魂伴侣", "2026"},
		{"《24小时：末路重生》2017.1080p", "24小时：末路重生", "2017"},
		{"《2012》1080p", "2012", ""},
	}
	for _, c := range cases {
		n, y := parseTitle(c.in)
		if n != c.name || y != c.year {
			t.Errorf("parseTitle(%q) = (%q,%q), want (%q,%q)", c.in, n, y, c.name, c.year)
		}
	}
}

func TestParseSeasonArabic(t *testing.T) {
	if n := ParseSeason("某剧 第2季 全12集"); n != 2 {
		t.Fatalf("第2季 = %d, want 2", n)
	}
	if n := ParseSeason("S02E05.1080p"); n != 2 {
		t.Fatalf("S02E05 = %d", n)
	}
	if n, ok := ParseSeasonOK("第二季"); !ok || n != 2 {
		t.Fatalf("ParseSeasonOK(第二季) = %d,%v", n, ok)
	}
	if n, ok := ParseSeasonOK("灵魂伴侣 (2026)"); ok {
		t.Fatalf("no-season title should report ok=false, got %d", n)
	}
}
