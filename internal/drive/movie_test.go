package drive

import (
	"testing"

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

	// 无法区分：序号兜底 -2
	g = &movieGroup{
		Title: "变形金刚",
		Year:  "2007",
		Files: []*userfile.File{
			file("变形金刚.2007.1080p.A.mkv"),
			file("变形金刚.2007.1080p.B.mkv"),
		},
		Names: []movieName{
			{Title: "变形金刚", Year: "2007", Quality: "1080p"},
			{Title: "变形金刚", Year: "2007", Quality: "1080p"},
		},
	}
	used = map[string]bool{}
	if got := pickMovieName(g, 0, "变形金刚 (2007)", used); got != "变形金刚 (2007).mkv" {
		t.Fatalf("first = %q", got)
	}
	if got := pickMovieName(g, 1, "变形金刚 (2007)", used); got != "变形金刚 (2007)-2.mkv" {
		t.Fatalf("second = %q", got)
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
