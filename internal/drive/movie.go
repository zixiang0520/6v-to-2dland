package drive

import (
	"context"
	"fmt"
	"log"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"6v-to-2dland/internal/tmdb"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

// 电影整理：BT 资源名普遍是「片名.年份.技术串」。同一个任务目录里可能装着一整个
// 系列合集（指环王 1/2/3）或多部混装，此时必须按片名各建目录；否则所有文件会被
// 塞进同一个标题目录，只能靠「标题-2」「标题-3」区分，媒体库识别成同一部片的多版本。
//
// 本文件的职责：从文件名解析片名/年份/盘号/分辨率 → 按片名分组 → 生成目标目录名与文件名。

var (
	// siteTagRe 匹配方括号/圆括号里的站点广告段，如「[电影天堂www.dytt89.com]」「【更多高清www.6vhao.com】」。
	siteTagRe = regexp.MustCompile(`(?i)[\[【（(][^\]】）)]*(?:www\.|\.com|\.net|\.cn|dytt|dy2018|6vhao|6v电影|6v下载|电影天堂|迅雷|磁力|BT下载|高清下载|地址发布)[^\]】）)]*[\]】）)]`)

	// yearNumRe 只匹配 4 位年份数字本体（1900-2099），边界由代码判断，
	// 避免正则带前后界导致连续两个年份只命中第一个（如 2012.2009）。
	yearNumRe = regexp.MustCompile(`(?:19|20)\d{2}`)

	// partRe 匹配分盘标记：CD1/CD2、Disc1、Part.2、DVD1。
	partRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:cd|disc|disk|part|pt|dvd)(\d{1,2})(?:[^a-z0-9]|$)`)

	// cnPartRe 匹配中文上下部标记：上部/下部/上集/下集/前篇/后篇。
	cnPartRe = regexp.MustCompile(`(上|下|前|后)(?:部|集|篇|编)`)

	// qualityRe 匹配分辨率标记。
	qualityRe = regexp.MustCompile(`(?i)(2160p|1080p|1080i|720p|576p|480p|4k|8k|uhd)`)

	// resTokenRe 匹配残留的 720p/1080i 之类孤立 token。
	resTokenRe = regexp.MustCompile(`(?i)^\d{3,4}[pi]$`)

	// partTokenRe 匹配孤立的分盘标记 token：CD1/CD2/Part1。
	partTokenRe = regexp.MustCompile(`(?i)^(?:cd|disc|disk|part|pt|dvd)\d{1,2}$`)

	// cnPartTokenRe 匹配孤立的中文分盘标记 token：上部/下集/前篇。
	cnPartTokenRe = regexp.MustCompile(`^(?:上|下|前|后)(?:部|集|篇|编)$`)

	// dupSuffixRe 匹配整理器自己加的序号后缀（-2/-3…）。整理是幂等的：
	// 第二次整理读到「片名-2.mp4」时必须还原成同一片名，否则每跑一次就多出一层目录。
	dupSuffixRe = regexp.MustCompile(`[-_]\d{1,2}$`)

	// qualityTailRe 匹配整理器加在片名末尾的分辨率后缀（片名-1080p），同样要能还原。
	qualityTailRe = regexp.MustCompile(`(?i)[-_ ](?:2160p|1080p|1080i|720p|576p|480p|4k|8k|uhd)$`)

	// langOnlyRe 匹配只由语言/字幕标记组成的 token（国英台粤四语、中英双字、国粤双语…）。
	// BT 资源里这些标记常和 BD/HD 连写成「BD中英双字」，词典式精确匹配抓不到。
	langOnlyRe = regexp.MustCompile(`^(?:[国粤英台日韩中法俄]|[一二三四五六七八两多]语|双语|双字|中字|字幕|原声|配音)+$`)
)

// techSubstrs 是需要从 token 内部剥掉的技术词（长词在前，避免 bluray 被 bd 先吃掉）。
var techSubstrs = []string{
	"bluray", "bdrip", "brrip", "webrip", "webdl", "hdtv", "hdrip",
	"x264", "x265", "h264", "h265", "hevc", "avc", "uhd", "bd", "hd", "web",
	"高清", "超清", "蓝光", "原盘", "高码", "4k", "8k",
}

// techWords 是 BT 文件名里的技术/语言/字幕词，解析片名时丢弃。
var techWords = map[string]bool{
	"bd": true, "bluray": true, "blu": true, "bdrip": true, "brrip": true,
	"webrip": true, "webdl": true, "web": true, "hdtv": true, "hdrip": true,
	"dvdrip": true, "remux": true, "hdr": true, "hdr10": true, "dv": true,
	"dolby": true, "vision": true, "atmos": true, "dts": true, "dtshd": true,
	"ac3": true, "aac": true, "truehd": true, "flac": true, "x264": true,
	"x265": true, "h264": true, "h265": true, "hevc": true, "avc": true,
	"10bit": true, "8bit": true, "60fps": true, "50fps": true, "complete": true,
	"repack": true, "proper": true, "extended": true, "unrated": true, "imax": true,
	"internal": true, "limited": true, "subs": true, "sub": true, "chs": true,
	"cht": true, "gb": true, "big5": true, "hd": true, "fhd": true, "uhd": true,
	"sd": true, "mp4": true, "mkv": true, "avi": true, "torrent": true,
	"国语": true, "粤语": true, "英语": true, "国英双语": true, "中英双字": true,
	"中字": true, "字幕": true, "简体": true, "繁体": true, "双语": true,
	"高清": true, "超清": true, "蓝光": true, "原盘": true, "修复版": true,
	"完整版": true, "未删减": true, "导演剪辑版": true, "加长版": true,
	"双字": true, "国语中字": true, "粤语中字": true, "中英字幕": true,
	"内嵌": true, "外挂": true, "无删减": true, "高码": true, "珍藏版": true,
}

// movieName 是从电影文件名解析出的元数据。
type movieName struct {
	Title   string // 片名（已清洗，可能同时含中英文）
	Year    string // 4 位年份，空表示解析不到
	Part    int    // 分盘序号（CD1=1、CD2=2），0 表示不分盘
	Quality string // 分辨率标记（1080p/2160p/4k…），空表示无
}

// movieGroup 是同一部电影的若干视频文件。
type movieGroup struct {
	Title string // 解析出的片名
	Year  string
	Keep  bool // 留在当前目录（与标题目录一致，或解析不出片名）
	Files []*userfile.File
	Names []movieName
}

// parseMovieName 解析电影文件名。
func parseMovieName(fileName string) movieName {
	var n movieName
	base := strings.TrimSuffix(fileName, path.Ext(fileName))
	base = siteTagRe.ReplaceAllString(base, " ")

	if m := qualityRe.FindStringSubmatch(base); m != nil {
		n.Quality = normalizeQuality(m[1])
	}
	// 整理必须是幂等的：先剥掉整理器自己加的 -2/-3 序号和 -1080p 分辨率后缀，
	// 否则第二次整理会把它们当成片名的一部分，每跑一次就多出一层「片名-2」目录。
	base = strings.TrimSpace(dupSuffixRe.ReplaceAllString(strings.TrimSpace(base), ""))
	base = strings.TrimSpace(qualityTailRe.ReplaceAllString(strings.TrimSpace(base), ""))
	if m := partRe.FindStringSubmatch(base); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil && v > 0 && v < 100 {
			n.Part = v
		}
	} else if m := cnPartRe.FindStringSubmatch(base); m != nil {
		if m[1] == "上" || m[1] == "前" {
			n.Part = 1
		} else {
			n.Part = 2
		}
	}

	// 片名里可能自带年份（《2001太空漫游》《2012》），而资源名的年份通常在片名之后，
	// 所以取最后一个年份作为分界点。
	titleEnd := -1
	for _, loc := range yearNumRe.FindAllStringIndex(base, -1) {
		if loc[0] > 0 && isASCIIDigit(base[loc[0]-1]) {
			continue
		}
		if loc[1] < len(base) && isASCIIDigit(base[loc[1]]) {
			continue
		}
		n.Year = base[loc[0]:loc[1]]
		titleEnd = loc[0]
	}
	head := base
	if titleEnd >= 0 {
		head = base[:titleEnd]
	}
	if t := cleanMovieTitle(head); t != "" {
		n.Title = t
		return n
	}
	// 年份在片名之前（少见），退回取年份之后的部分。
	if n.Year != "" {
		if i := strings.Index(base, n.Year); i >= 0 {
			n.Title = cleanMovieTitle(base[i+len(n.Year):])
		}
	}
	return n
}

// isASCIIDigit 判断字节是否为 ASCII 数字。
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// normalizeQuality 统一分辨率写法。
func normalizeQuality(s string) string {
	q := strings.ToLower(s)
	switch q {
	case "4k", "8k", "uhd":
		if q == "uhd" {
			return "4k"
		}
		return q
	}
	return q
}

// cleanMovieTitle 去掉技术词并把分隔符还原成空格。
func cleanMovieTitle(s string) string {
	toks := splitTitleTokens(s)
	kept := make([]string, 0, len(toks))
	for _, t := range toks {
		if isTechToken(t) {
			continue
		}
		kept = append(kept, t)
	}
	out := strings.TrimSpace(strings.Join(kept, " "))
	out = strings.Trim(out, " -_—–:：,，、~～.")
	return out
}

// splitTitleTokens 按常见分隔符切分标题；保留 - 以免拆散 Spider-Man 这类名字。
func splitTitleTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '.', '_', '+', '·', '|', '/', '\\', ' ', '\t', '\u3000',
			'(', ')', '（', '）', '[', ']', '【', '】':
			return true
		}
		return false
	})
}

// isTechToken 判断 token 是否是纯技术标记。
func isTechToken(t string) bool {
	low := strings.ToLower(strings.Trim(t, " -_"))
	if low == "" {
		return true
	}
	if techWords[low] {
		return true
	}
	if resTokenRe.MatchString(low) {
		return true
	}
	if partTokenRe.MatchString(low) || cnPartTokenRe.MatchString(low) {
		return true
	}
	if qualityRe.MatchString(low) && len(low) <= len("2160p") {
		return true
	}
	// 连写标记：BD中英双字 / 国英台粤四语 —— 剥掉技术词后什么都不剩，或整 token 就是
	// 语言字幕标记组合（必须含「语/字/幕」以防误杀《中国》《英国病人》这类片名）。
	if stripTechSubstr(low) == "" {
		return true
	}
	if s := stripTechSubstr(low); langOnlyRe.MatchString(s) && strings.ContainsAny(s, "语字幕") {
		return true
	}
	return false
}

// stripTechSubstr 从 token 内部剥掉技术词，返回剩余部分。
func stripTechSubstr(s string) string {
	for _, w := range techSubstrs {
		s = strings.ReplaceAll(s, w, "")
	}
	return strings.Trim(s, " -_.")
}

// normalizeTitleKey 生成用于比较的归一化片名：只保留字母、数字、汉字。
func normalizeTitleKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// groupMovies 按「片名 + 年份」把视频文件分组。
func groupMovies(videos []*userfile.File, titleDir string) []*movieGroup {
	baseKey := normalizeTitleKey(stripYear(titleDir))
	var groups []*movieGroup
	index := map[string]*movieGroup{}
	for _, v := range videos {
		n := parseMovieName(v.Name)
		key := "?"
		if n.Title != "" {
			key = normalizeTitleKey(n.Title) + "|" + n.Year
		}
		g := index[key]
		if g == nil {
			g = &movieGroup{Title: n.Title, Year: n.Year}
			index[key] = g
			groups = append(groups, g)
		}
		g.Files = append(g.Files, v)
		g.Names = append(g.Names, n)
	}
	for _, g := range groups {
		// 解析不出片名，或片名与当前目录（去掉年份后）一致 → 留原地。
		if g.Title == "" || normalizeTitleKey(g.Title) == baseKey {
			g.Keep = true
		}
	}
	// 整个目录只有一部片时不拆目录：即便解析名与目录名有出入（中英混排、简繁差异），
	// 搬家只会制造混乱。只有多部混装/系列合集才按片名拆分。
	if len(groups) == 1 && len(groups[0].Files) == len(videos) {
		groups[0].Keep = true
	}
	return groups
}

// dirName 返回该组对应的标题目录名。
func (g *movieGroup) dirName(titleDir string) string {
	if g.Keep || g.Title == "" {
		return titleDir
	}
	if g.Year != "" {
		return sanitize(g.Title + " (" + g.Year + ")")
	}
	return sanitize(g.Title)
}

// pickMovieName 生成组内第 i 个文件的目标文件名。
// 单文件 → 「标题 (年份).ext」；同组多文件（多盘/多版本）→ 用 -CDn / -分辨率 区分，
// 分辨率重复时再追加序号（-1080p / -1080p-2），保证名字自带信息且反复整理可收敛。
func pickMovieName(g *movieGroup, i int, dirTitle string, used map[string]bool) string {
	ext := path.Ext(g.Files[i].Name)
	base := dirTitle
	suffix := ""
	if len(g.Files) > 1 {
		n := g.Names[i]
		switch {
		case n.Part > 0 && uniqueParts(g.Names):
			suffix = fmt.Sprintf("-CD%d", n.Part)
		case n.Quality != "":
			suffix = "-" + n.Quality
		}
	}
	cand := base + suffix + ext
	if !used[strings.ToLower(cand)] {
		used[strings.ToLower(cand)] = true
		return cand
	}
	for k := 2; ; k++ {
		cand = fmt.Sprintf("%s%s-%d%s", base, suffix, k, ext)
		if !used[strings.ToLower(cand)] {
			used[strings.ToLower(cand)] = true
			return cand
		}
	}
}

// uniqueParts 判断组内分盘号是否互不相同（都非 0）。
func uniqueParts(names []movieName) bool {
	seen := map[int]bool{}
	for _, n := range names {
		if n.Part == 0 || seen[n.Part] {
			return false
		}
		seen[n.Part] = true
	}
	return true
}

// uniqueQualities 判断组内分辨率标记是否互不相同（都非空）。
func uniqueQualities(names []movieName) bool {
	seen := map[string]bool{}
	for _, n := range names {
		if n.Quality == "" || seen[n.Quality] {
			return false
		}
		seen[n.Quality] = true
	}
	return true
}

// organizeMovies 整理电影类目录下的视频：
//   - 与当前标题目录一致（或解析不出片名）的留在原目录；
//   - 解析出不同片名的（系列合集、多部混装），按片名各自新建目录并搬进去。
//
// 返回新建的目录列表。
func (c *Client) organizeMovies(ctx context.Context, savePath string, res *OrganizeResult, videos []*userfile.File, titleDir string) []string {
	groups := groupMovies(videos, titleDir)
	parentPath := path.Dir(savePath)
	s := c.snap()
	var splitDirs []string
	usedByDir := map[string]map[string]bool{}
	// Keep 组（整个目录只有一部片、片名与目录一致）也要过一遍 TMDB：
	// 6v 页面标题常不带年份、或中文名与 TMDB 词条不一致，否则这类目录永远「裸名」。
	// TMDB 命中后原地把目录与文件改成「片名 (年份)」，仍在本目录内，不算拆分。
	keepDirTitle := titleDir
	if len(groups) == 1 && groups[0].Keep && len(groups[0].Files) == len(videos) {
		g := groups[0]
		title := g.Title
		if title == "" {
			// 文件名解析不出片名时退回目录名（去年份），别浪费 TMDB 这次查询。
			title = stripYear(titleDir)
		}
		// Keep 组（单片目录）只补缺失的年份，不改片名：
		// 目录名沿用现有叫法（6v 名或用户手动纠正过的名字不能被 TMDB 词条名顶掉，
		// 如「守护星」↔「守护你，遇见爱」）。目录已带年份则完全跳过——幂等。
		if stripYear(titleDir) == titleDir {
			if norm := tmdbDirName(ctx, s.tmdb, title, g.Year); norm != "" {
				if y := yearFromDirName(norm); y != "" {
					log.Printf("organizeMovies: keep-group year %q -> %q (tmdb %q)", titleDir, sanitize(title+" ("+y+")"), norm)
					keepDirTitle = sanitize(title + " (" + y + ")")
				}
			}
		}
	}
	for _, g := range groups {
		dest := savePath
		dirTitle := titleDir
		if !g.Keep {
			dirTitle = g.dirName(titleDir)
			// 本地解析只拿到片名，年份要靠 TMDB 补；查不到就维持本地结果。
			if norm := tmdbDirName(ctx, s.tmdb, g.Title, g.Year); norm != "" {
				log.Printf("organizeMovies: tmdb %q (year %q) -> %q", g.Title, g.Year, norm)
				dirTitle = norm
			}
			if strings.TrimSpace(dirTitle) == "" {
				dirTitle = titleDir
			}
			p, err := c.ensureDirUnder(ctx, parentPath, dirTitle)
			if err != nil {
				log.Printf("organizeMovies: ensure dir %q under %q failed: %v", dirTitle, parentPath, err)
				for _, f := range g.Files {
					res.Skipped = append(res.Skipped, f.Name+" (创建目录失败)")
				}
				continue
			}
			dest = p
			splitDirs = append(splitDirs, p)
			log.Printf("organizeMovies: split %q -> %q", g.Title, p)
		}
		if g.Keep && keepDirTitle != titleDir {
			// 单片目录规范化：目录改名（2dland Rename 走 identity），文件随之用新标题重命名。
			dirTitle = keepDirTitle
			if err := c.renameDir(ctx, savePath, keepDirTitle); err != nil {
				log.Printf("organizeMovies: rename dir %q -> %q failed: %v", savePath, keepDirTitle, err)
				dirTitle = titleDir // 目录改名失败就别改文件名，保持目录/文件一致
			} else {
				res.Renamed = append(res.Renamed, RenameRecord{Old: path.Base(savePath), New: keepDirTitle})
				res.ArchivePath = path.Join(parentPath, keepDirTitle)
				res.SavePath = res.ArchivePath
				log.Printf("organizeMovies: keep dir renamed %q -> %q", savePath, res.ArchivePath)
			}
		}
		used := usedByDir[dest]
		if used == nil {
			used = map[string]bool{}
			usedByDir[dest] = used
		}
		for i, f := range g.Files {
			newName := pickMovieName(g, i, dirTitle, used)
			if newName != f.Name {
				if err := c.Rename(ctx, f.Identity, newName); err != nil {
					log.Printf("organizeMovies: rename %q -> %q failed: %v", f.Name, newName, err)
					res.Skipped = append(res.Skipped, f.Name+" (重命名失败)")
					continue
				}
				log.Printf("organizeMovies: rename %q -> %q", f.Name, newName)
				res.Renamed = append(res.Renamed, RenameRecord{Old: f.Name, New: newName})
			}
			if dest != savePath {
				if err := c.Move(ctx, []string{f.Identity}, dest); err != nil {
					log.Printf("organizeMovies: move %q -> %q failed: %v", newName, dest, err)
					res.Skipped = append(res.Skipped, newName+" (移动失败)")
				}
			}
		}
	}
	// 视频全部拆到别的目录后，原项目目录会空掉。每推送一次就多一层「0 文件」空壳，
	// 媒体库里看着像幽灵条目，这里顺手清掉（进回收站，可恢复）。
	if len(splitDirs) > 0 {
		c.removeDirIfEmpty(ctx, savePath)
	}
	return splitDirs
}

// renameDir 原地重命名目录（保持位置不变，仅改名字）。
func (c *Client) renameDir(ctx context.Context, dirPath, newName string) error {
	s := c.snap()
	d, err := s.userfile.Get(ctx, &userfile.File{Path: dirPath})
	if err != nil || d == nil || d.Identity == "" {
		return fmt.Errorf("查询目录 %q 失败: %w", dirPath, err)
	}
	if err := c.Rename(ctx, d.Identity, newName); err != nil {
		return err
	}
	// Rename 后旧 path 缓存会失效，重新 Get 一次确认生效（失败只记日志，不阻断）。
	if _, err := s.userfile.Get(ctx, &userfile.File{Path: path.Join(path.Dir(dirPath), newName)}); err != nil {
		log.Printf("renameDir: confirm %q failed: %v", newName, err)
	}
	return nil
}

// yearFromDirName 从「片名 (YYYY)」形态的目录名里取年份；无则空串。
func yearFromDirName(name string) string {
	m := yearSuffixRe.FindStringSubmatch(name)
	if len(m) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Trim(m[0], "() \t"))
}

// tmdbDirName 用 TMDB 把解析出的片名规范成「片名 (年份)」。
// 查不到、匹配不可靠或 TMDB 未配置时返回空串，调用方回退到本地解析结果——
// 宁可目录名朴素，也不要张冠李戴。
func tmdbDirName(ctx context.Context, t *tmdb.Client, title, year string) string {
	if t == nil || strings.TrimSpace(title) == "" {
		return ""
	}
	r, err := t.Best(ctx, title, "movie", year)
	if err != nil {
		log.Printf("tmdb: search %q (year %q) failed: %v", title, year, err)
		return ""
	}
	if r == nil && year != "" {
		// 文件名里的年份常是压制/发布年，对不上就退回不带年份再试一次。
		if r2, err2 := t.Best(ctx, title, "movie", ""); err2 == nil {
			r = r2
		}
	}
	if r == nil || r.Title == "" {
		return ""
	}
	y := tmdb.YearFromDate(r.Date)
	if y == "" {
		y = year
	}
	if y != "" {
		return sanitize(r.Title + " (" + y + ")")
	}
	return sanitize(r.Title)
}

// ensureDirUnder 在 parentPath 下确保 name 目录存在，返回完整路径。
func (c *Client) ensureDirUnder(ctx context.Context, parentPath, name string) (string, error) {
	s := c.snap()
	parent, err := s.userfile.Get(ctx, &userfile.File{Path: parentPath})
	if err != nil || parent == nil || parent.Identity == "" {
		return "", fmt.Errorf("父目录 %q 不存在: %v", parentPath, err)
	}
	p, _, err := ensureDir(ctx, s.userfile, parentPath, parent.Identity, name)
	return p, err
}
