package drive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"6v-to-2dland/internal/tmdb"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

func TestParseMovieName(t *testing.T) {
	cases := []struct {
		in    string
		title string
		year  string
		part  int
		qual  string
	}{
		{
			in:    "指环王1：护戒使者.The.Lord.of.the.Rings.2001.BD1080P.国英双语.中英双字.mp4",
			title: "指环王1：护戒使者 The Lord of the Rings",
			year:  "2001",
			qual:  "1080p",
		},
		{
			in:    "[电影天堂www.dytt89.com]指环王1：护戒使者-2001_BD国语中字.mp4",
			title: "指环王1：护戒使者",
			year:  "2001",
		},
		{
			in:    "变形金刚.2007.2160p.HDR.国英双语.mp4",
			title: "变形金刚",
			year:  "2007",
			qual:  "2160p",
		},
		{
			// 片名自带年份，资源年份取最后一个
			in:    "2001太空漫游.1968.1080p.mkv",
			title: "2001太空漫游",
			year:  "1968",
			qual:  "1080p",
		},
		{
			// 片名就是年份数字
			in:    "2012.2009.720p.mkv",
			title: "2012",
			year:  "2009",
			qual:  "720p",
		},
		{
			in:    "指环王.王者无敌.CD1.2003.mkv",
			title: "指环王 王者无敌",
			year:  "2003",
			part:  1,
		},
		{
			in:    "霍比特人2.The.Desolation.of.Smaug.2013.1080p.BluRay.x264.mkv",
			title: "霍比特人2 The Desolation of Smaug",
			year:  "2013",
			qual:  "1080p",
		},
		{
			// 已整理的规范名：重新整理时不能把片名解析成空
			in:    "指环王 (2001)-2.mkv",
			title: "指环王",
			year:  "2001",
		},
		{
			in:    "无年份.1080p.mp4",
			title: "无年份",
			qual:  "1080p",
		},
		{
			// 连写标记：BD中英双字 不能留在片名里
			in:    "玩具总动员4.720p.BD中英双字[66影视www.66Ys.Co].mp4",
			title: "玩具总动员4",
			qual:  "720p",
		},
		{
			// 连写语言标记：国英台粤四语
			in:    "玩具总动员.1080p.国英台粤四语.BD中英双字[最新电影www.66e.cc].mp4",
			title: "玩具总动员",
			qual:  "1080p",
		},
		{
			// 幂等：整理器自己加的 -2 后缀不能变成片名的一部分
			in:    "玩具总动员4 BD中英双字-2.mp4",
			title: "玩具总动员4",
		},
		{
			// 幂等：分辨率后缀 + 序号也要能还原
			in:    "玩具总动员4-1080p-2.mp4",
			title: "玩具总动员4",
			qual:  "1080p",
		},
		{
			// 片名带空格数字（续集号）不能被当成去重后缀吃掉
			in:    "Toy Story 2.1999.1080p.mkv",
			title: "Toy Story 2",
			year:  "1999",
			qual:  "1080p",
		},
	}
	for _, c := range cases {
		got := parseMovieName(c.in)
		if got.Title != c.title {
			t.Errorf("parseMovieName(%q).Title = %q, want %q", c.in, got.Title, c.title)
		}
		if got.Year != c.year {
			t.Errorf("parseMovieName(%q).Year = %q, want %q", c.in, got.Year, c.year)
		}
		if got.Part != c.part {
			t.Errorf("parseMovieName(%q).Part = %d, want %d", c.in, got.Part, c.part)
		}
		if got.Quality != c.qual {
			t.Errorf("parseMovieName(%q).Quality = %q, want %q", c.in, got.Quality, c.qual)
		}
	}
}

func file(name string) *userfile.File {
	return &userfile.File{Name: name, Path: "/6v下载/电影/合集/" + name, Identity: name}
}

func TestGroupMoviesSplitsSeries(t *testing.T) {
	videos := []*userfile.File{
		file("指环王1：护戒使者.2001.1080p.mkv"),
		file("指环王2：双塔奇兵.2002.1080p.mkv"),
		file("指环王3：王者无敌.2003.1080p.mkv"),
	}
	groups := groupMovies(videos, "指环王三部曲 (2020)")
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	for _, g := range groups {
		if g.Keep {
			t.Errorf("group %q should be split out, got Keep=true", g.Title)
		}
	}
}

func TestGroupMoviesKeepsSingleMovie(t *testing.T) {
	videos := []*userfile.File{
		file("指环王1：护戒使者.The.Lord.of.the.Rings.2001.1080p.mkv"),
	}
	groups := groupMovies(videos, "指环王 (2001)")
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if !groups[0].Keep {
		t.Fatalf("single movie group should keep current dir")
	}
}

func TestGroupMoviesMultiDiscSameMovie(t *testing.T) {
	videos := []*userfile.File{
		file("指环王3.王者无敌.CD1.2003.mkv"),
		file("指环王3.王者无敌.CD2.2003.mkv"),
	}
	groups := groupMovies(videos, "指环王3：王者无敌 (2003)")
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (same movie)", len(groups))
	}
	if len(groups[0].Files) != 2 {
		t.Fatalf("files = %d, want 2", len(groups[0].Files))
	}
}

func TestPickMovieName(t *testing.T) {
	// 单文件：标题 (年份).ext
	g := &movieGroup{
		Title: "指环王1：护戒使者",
		Year:  "2001",
		Files: []*userfile.File{file("指环王1：护戒使者.2001.1080p.mkv")},
		Names: []movieName{{Title: "指环王1：护戒使者", Year: "2001", Quality: "1080p"}},
	}
	if got := pickMovieName(g, 0, "指环王1：护戒使者 (2001)", map[string]bool{}); got != "指环王1：护戒使者 (2001).mkv" {
		t.Fatalf("single = %q", got)
	}

	// 多盘：-CD1/-CD2
	g = &movieGroup{
		Title: "指环王3 王者无敌",
		Year:  "2003",
		Files: []*userfile.File{
			file("指环王3.王者无敌.CD1.2003.mkv"),
			file("指环王3.王者无敌.CD2.2003.mkv"),
		},
		Names: []movieName{
			{Title: "指环王3 王者无敌", Year: "2003", Part: 1},
			{Title: "指环王3 王者无敌", Year: "2003", Part: 2},
		},
	}
	used := map[string]bool{}
	if got := pickMovieName(g, 0, "指环王3 王者无敌 (2003)", used); got != "指环王3 王者无敌 (2003)-CD1.mkv" {
		t.Fatalf("cd1 = %q", got)
	}
	if got := pickMovieName(g, 1, "指环王3 王者无敌 (2003)", used); got != "指环王3 王者无敌 (2003)-CD2.mkv" {
		t.Fatalf("cd2 = %q", got)
	}

	// 多版本：-分辨率
	g = &movieGroup{
		Title: "变形金刚",
		Year:  "2007",
		Files: []*userfile.File{
			file("变形金刚.2007.1080p.mkv"),
			file("变形金刚.2007.2160p.mkv"),
		},
		Names: []movieName{
			{Title: "变形金刚", Year: "2007", Quality: "1080p"},
			{Title: "变形金刚", Year: "2007", Quality: "2160p"},
		},
	}
	used = map[string]bool{}
	if got := pickMovieName(g, 0, "变形金刚 (2007)", used); got != "变形金刚 (2007)-1080p.mkv" {
		t.Fatalf("1080p = %q", got)
	}
	if got := pickMovieName(g, 1, "变形金刚 (2007)", used); got != "变形金刚 (2007)-2160p.mkv" {
		t.Fatalf("2160p = %q", got)
	}

	// 无法区分（无分辨率可依）：序号兜底 -2
	g = &movieGroup{
		Title: "变形金刚",
		Year:  "2007",
		Files: []*userfile.File{
			file("变形金刚.2007.DTS.x264.A.mkv"),
			file("变形金刚.2007.DTS.x264.B.mkv"),
		},
		Names: []movieName{
			{Title: "变形金刚", Year: "2007"},
			{Title: "变形金刚", Year: "2007"},
		},
	}
	used = map[string]bool{}
	if got := pickMovieName(g, 0, "变形金刚 (2007)", used); got != "变形金刚 (2007).mkv" {
		t.Fatalf("first = %q", got)
	}
	if got := pickMovieName(g, 1, "变形金刚 (2007)", used); got != "变形金刚 (2007)-2.mkv" {
		t.Fatalf("second = %q", got)
	}

	// 分辨率重复（同片两个 1080p 版本）：-1080p / -1080p-2，而不是裸序号
	g = &movieGroup{
		Title: "玩具总动员4",
		Files: []*userfile.File{
			file("a.mp4"), file("b.mp4"),
		},
		Names: []movieName{
			{Title: "玩具总动员4", Quality: "1080p"},
			{Title: "玩具总动员4", Quality: "1080p"},
		},
	}
	used = map[string]bool{}
	if got := pickMovieName(g, 0, "玩具总动员4", used); got != "玩具总动员4-1080p.mp4" {
		t.Fatalf("dup quality first = %q", got)
	}
	if got := pickMovieName(g, 1, "玩具总动员4", used); got != "玩具总动员4-1080p-2.mp4" {
		t.Fatalf("dup quality second = %q", got)
	}
}

func TestGroupMoviesMixedWithUnknownKeepsUnknown(t *testing.T) {
	videos := []*userfile.File{
		file("指环王1：护戒使者.2001.1080p.mkv"),
		file("指环王2：双塔奇兵.2002.1080p.mkv"),
		file("1080p.mkv"), // 解析不出片名
	}
	groups := groupMovies(videos, "指环王合集 (2020)")
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	for _, g := range groups {
		if g.Title == "" && !g.Keep {
			t.Fatal("unknown-title group must keep current dir")
		}
	}
}

func TestGroupMoviesSameTitleDifferentYear(t *testing.T) {
	// 同名不同年份（翻拍版）必须拆成两个目录，不能合并成 -2
	videos := []*userfile.File{
		file("倩女幽魂.1987.1080p.mkv"),
		file("倩女幽魂.2011.1080p.mkv"),
	}
	groups := groupMovies(videos, "倩女幽魂合集")
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (different years)", len(groups))
	}
	if groups[0].Keep || groups[1].Keep {
		t.Fatal("different-year remakes must be split, not numbered")
	}
	if got := groups[1].dirName("倩女幽魂合集"); got != "倩女幽魂 (2011)" {
		t.Fatalf("dirName = %q", got)
	}
}

func TestMovieDirNameWithoutYear(t *testing.T) {
	g := &movieGroup{Title: "指环王1：护戒使者"}
	if got := g.dirName("系列合集 (2020)"); got != "指环王1：护戒使者" {
		t.Fatalf("dirName without year = %q", got)
	}
	// 解析得出的标题要经过 sanitize 清理非法字符
	g = &movieGroup{Title: "A/B.Test", Year: "2001"}
	if got := g.dirName("x"); got != "A_B.Test (2001)" {
		t.Fatalf("dirName sanitize = %q", got)
	}
}

func TestParseMovieNameChinesePart(t *testing.T) {
	n := parseMovieName("指环王3.王者无敌.下部.2003.1080p.mkv")
	if n.Part != 2 {
		t.Fatalf("part = %d, want 2", n.Part)
	}
	if n.Title != "指环王3 王者无敌" {
		t.Fatalf("title = %q", n.Title)
	}
	if n.Year != "2003" {
		t.Fatalf("year = %q", n.Year)
	}
}

// 同片多版本（720p/1080p、国英双语/中英双字）必须收敛到同一个目录，
// 不能每次整理都新建「片名-2/-3/-4」目录。
func TestGroupMoviesSameMovieMultiVersions(t *testing.T) {
	videos := []*userfile.File{
		file("玩具总动员4.1080p.BD中英双字[66影视www.66Ys.Co].mp4"),
		file("玩具总动员4.1080p.国英双语.BD中英双字[66影视www.66Ys.Co].mp4"),
		file("玩具总动员4.720p.BD中英双字[66影视www.66Ys.Co].mp4"),
		file("玩具总动员4.720p.国英双语.BD中英双字[66影视www.66Ys.Co].mp4"),
	}
	groups := groupMovies(videos, "玩具总动员四部全")
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (same movie, multi versions)", len(groups))
	}
	g := groups[0]
	if len(g.Files) != 4 {
		t.Fatalf("files = %d, want 4", len(g.Files))
	}
	k := normalizeTitleKey(g.Title)
	if !strings.HasPrefix(k, "玩具总动员4") || strings.Contains(k, "bd") || strings.Contains(k, "中英双字") {
		t.Fatalf("parsed title still dirty: %q", g.Title)
	}
	used := map[string]bool{}
	got := make([]string, 0, len(g.Files))
	for i := range g.Files {
		got = append(got, pickMovieName(g, i, "玩具总动员4", used))
	}
	want := []string{
		"玩具总动员4-1080p.mp4",
		"玩具总动员4-1080p-2.mp4",
		"玩具总动员4-720p.mp4",
		"玩具总动员4-720p-2.mp4",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("name[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

// 整理阶段也要向 TMDB 要年份，否则目录只能叫「玩具总动员4」没有 (2019)。
func TestTmdbDirName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"玩具总动员4","original_title":"Toy Story 4","release_date":"2019-06-19"}]}`))
	}))
	defer srv.Close()

	c := tmdb.NewWithBase("key", "", "zh-CN", srv.URL)
	if got := tmdbDirName(context.Background(), c, "玩具总动员4", ""); got != "玩具总动员4 (2019)" {
		t.Fatalf("tmdbDirName = %q, want 玩具总动员4 (2019)", got)
	}
	// 未配置 TMDB 时返回空串，由调用方回退本地解析结果
	if got := tmdbDirName(context.Background(), nil, "玩具总动员4", ""); got != "" {
		t.Fatalf("nil client = %q, want empty", got)
	}
}

func TestYearFromDirName(t *testing.T) {
	cases := map[string]string{
		"守护星 (2026)":   "2026",
		"马登 (2023)":    "2023",
		"守护星":          "",
		"已下载":          "",
		"完美说谎者 (2026)": "2026",
	}
	for in, want := range cases {
		if got := yearFromDirName(in); got != want {
			t.Fatalf("yearFromDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Keep 组（单片目录）缺年份时由 TMDB 补：「守护星」→「守护星 (2026)」，片名保持 6v 原名。
func TestKeepGroupYearEnrichment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟别名命中：6v 名搜不到，TMDB 词条名不同但年份可取。
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"守护你，遇见爱","original_title":"Guarding Stars","release_date":"2026-11-18"}]}`))
	}))
	defer srv.Close()

	tc := tmdb.NewWithBase("key", "", "zh-CN", srv.URL)
	tc.SetAliases(map[string]string{"守护星": "守护你，遇见爱"})

	norm := tmdbDirName(context.Background(), tc, "守护星", "")
	if norm == "" {
		t.Fatal("alias should hit")
	}
	if y := yearFromDirName(norm); y != "2026" {
		t.Fatalf("year = %q, want 2026", y)
	}
	// 组装后的目录名应保留 6v 原名并补年份
	if got := sanitize("守护星 (2026)"); got != "守护星 (2026)" {
		t.Fatalf("final dir = %q", got)
	}
}
