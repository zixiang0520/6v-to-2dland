package drive

import "testing"

func TestZYDYIsTVCategory(t *testing.T) {
	if !isTVCategory("zydy") {
		t.Fatal("zydy (动漫) must be treated as TV so organize keeps episode numbers")
	}
	if isTVCategory("jddy") {
		t.Fatal("jddy (动画电影) should stay movie")
	}
	if isTVCategory("dy") {
		t.Fatal("dy should stay movie")
	}
}

func TestParseEpisodeAnimeNames(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"X战警97.X-Men.97.S02E05.1080p.HD中英双字.mp4", 5},
		{"X-Men.97.S02E01-03.1080p.mp4", 1}, // 合集取起始集，不误取 03
		{"记忆管理局.更新04.1080p.mp4", 4},
		{"将夜(动画版).第17话.mp4", 17},
		{"将夜.第17集.mkv", 17},
		{"吞噬星空.EP236.mp4", 236},
		{"Show.Name.E05.1080p.mkv", 5},
		{"无集数信息.1080p.mp4", 0},
		{"动漫标题.1080p.HD中字.mp4", 0},
		{"动漫标题.720p.mkv", 0},
	}
	for _, c := range cases {
		got := parseEpisode(c.in)
		if got != c.want {
			t.Errorf("parseEpisode(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSeasonFromAnimeName(t *testing.T) {
	if n := parseSeasonFromName("X战警97.X-Men.97.S02E05.1080p.mp4"); n != 2 {
		t.Fatalf("S02E05 season = %d, want 2", n)
	}
	if n := parseSeasonFromName("记忆管理局.更新04.1080p.mp4"); n != 0 {
		t.Fatalf("no Sxx should be 0, got %d", n)
	}
}

func TestTVRenameKeepsEpisode(t *testing.T) {
	// 模拟整理命名：动漫必须走剧集格式，不能变成「标题 (年份).mp4」丢掉集数
	titleDir := "X战警97 (2024)"
	pure := stripYear(titleDir)
	ep := parseEpisode("X战警97.X-Men.97.S02E05.1080p.HD中英双字.mp4")
	if ep != 5 {
		t.Fatalf("ep=%d", ep)
	}
	got := pure + "S01E05.mp4" // season 从目录来，这里只钉集数进入新名
	if got != "X战警97S01E05.mp4" {
		t.Fatalf("rename = %q", got)
	}
	// 电影路径会丢掉集数——这就是 bug
	movieName := titleDir + ".mp4"
	if movieName == "X战警97 (2024).mp4" && !isTVCategory("zydy") {
		t.Fatal("zydy treated as movie would wipe episode")
	}
}
