package subscribe

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
	"6v-to-2dland/internal/fnos"
	"6v-to-2dland/internal/openlist"
	"6v-to-2dland/internal/site6v"
)

type CheckResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	OK     bool   `json:"ok"`
	Msg    string `json:"msg"`
	Pushed int    `json:"pushed"`
}

type Checker struct {
	Store   *Store
	Site    *site6v.Client
	Drive   *drive.Client
	GetCfg  func() cfg.Config
	GetOL   func() *openlist.Client
	GetFnos func() *fnos.Client
	mu      sync.Mutex
}

func (c *Checker) CheckAll(ctx context.Context) []CheckResult {
	return c.checkList(ctx, c.Store.snapshot(), "manual")
}

// CheckDue 只检查达到各自间隔的订阅。
func (c *Checker) CheckDue(ctx context.Context) []CheckResult {
	return c.checkList(ctx, c.Store.DueSnapshot(time.Now()), "auto")
}

func (c *Checker) checkList(ctx context.Context, subs []*Sub, trigger string) []CheckResult {
	if !c.mu.TryLock() {
		return []CheckResult{{OK: false, Msg: "已有检查任务正在运行"}}
	}
	defer c.mu.Unlock()
	var out []CheckResult
	for _, sub := range subs {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		out = append(out, c.checkSub(ctx, *sub, trigger))
		time.Sleep(2 * time.Second)
	}
	return out
}

func (c *Checker) CheckOne(ctx context.Context, id string) (CheckResult, error) {
	if !c.mu.TryLock() {
		return CheckResult{ID: id, OK: false, Msg: "已有检查任务正在运行"}, fmt.Errorf("已有检查任务正在运行")
	}
	defer c.mu.Unlock()
	sub, ok := c.Store.Get(id)
	if !ok {
		return CheckResult{ID: id}, fmt.Errorf("订阅不存在")
	}
	return c.checkSub(ctx, sub, "manual"), nil
}

func (c *Checker) checkSub(ctx context.Context, sub Sub, trigger string) CheckResult {
	result := CheckResult{ID: sub.ID, Title: sub.Title}
	entry := CheckLog{Trigger: trigger}
	mark := func(msg string, ok bool) CheckResult {
		result.Msg, result.OK = msg, ok
		entry.Message, entry.OK = msg, ok
		c.Store.AppendLog(sub.ID, entry)
		return result
	}
	if c.Site == nil || c.Drive == nil {
		return mark("服务依赖未初始化", false)
	}
	// 6v 固定详情页优先：URL 合法且仍能解析到磁力时，完全跳过关键词搜索。
	// 失效、404 或无磁力才回退搜索；搜索命中后自动回填固定链接。
	var matched []site6v.Resource
	fixedUsed := false
	if rawURL := strings.TrimSpace(sub.SixvURL); rawURL != "" && validSixvDetailURL(rawURL, c.Site.Base) {
		if magnets, err := c.Site.FetchMagnets(ctx, rawURL); err == nil && len(magnets) > 0 {
			matched = append(matched, site6v.Resource{Title: sub.Title, URL: rawURL, Category: sub.Category})
			fixedUsed = true
			log.Printf("subscription %s: use fixed 6v URL %s magnets=%d", sub.ID, rawURL, len(magnets))
		} else {
			log.Printf("subscription %s: fixed 6v URL invalid, fallback search: %v", sub.ID, err)
		}
	}
	if !fixedUsed {
		rs := c.Site.Search(ctx, sub.Keyword, 2)
		for _, r := range rs {
			if resourceMatches(sub.Keyword, r.Title) {
				matched = append(matched, r)
			}
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Date == "" {
			return false
		}
		if matched[j].Date == "" {
			return true
		}
		return matched[i].Date > matched[j].Date
	})
	if len(matched) > 5 {
		matched = matched[:5]
	}
	if len(matched) == 0 {
		return mark("6v 暂未找到匹配资源", true)
	}
	if sub.Type != "tv" {
		return c.checkMovie(ctx, sub, matched, &entry, mark)
	}

	// 各来源独立统计，不能因为另一来源已加入 have 就把数量扣成 0。
	pushedHave := map[string]bool{}
	for k, v := range sub.PushedEps {
		if v {
			pushedHave[k] = true
		}
	}
	cfgNow := cfg.Config{}
	if c.GetCfg != nil {
		cfgNow = c.GetCfg()
	}
	ol := (*openlist.Client)(nil)
	if c.GetOL != nil {
		ol = c.GetOL()
	}

	driveHave := map[string]bool{}
	driveCount, driveErr := c.driveHave(ctx, &sub, cfgNow, driveHave)
	if driveErr != nil {
		log.Printf("subscription %s: 2dland query failed, stop this check: %v", sub.ID, driveErr)
		return mark("2dland 当前任务/文件查询失败，为避免重复推送已停止本轮检查: "+driveErr.Error(), false)
	}

	fnosHave := map[string]bool{}
	fnosCount := 0
	fnosOK := false
	if c.GetFnos != nil && sub.FnosSeasonURL != "" {
		if fc := c.GetFnos(); fc != nil {
			var ferr error
			fnosCount, ferr = c.fnosHave(ctx, fc, sub.FnosSeasonURL, sub.Season, fnosHave)
			if ferr == nil {
				fnosOK = true
			} else {
				log.Printf("subscription %s: 飞牛查询失败，使用 OpenList 辅助: %v", sub.ID, ferr)
			}
		}
	}

	// OpenList 始终独立查询用于显示；作为辅助来源参与防重复。
	olHave := map[string]bool{}
	olCount := 0
	if ol != nil && ol.Configured() {
		var err error
		olCount, err = c.openlistHave(ctx, ol, cfgNow, &sub, olHave)
		if err != nil {
			log.Printf("subscription %s: OpenList query failed: %v", sub.ID, err)
		}
	}

	// 当前缺集只由“现在仍存在”的来源决定：飞牛/OpenList 媒体库，
	// 以及 2dland 当前任务和文件。历史 pushed/pushed_eps 仅用于日志统计，
	// 不能永久拦截；用户删除 2dland 内容后，应允许重新形成差集并补推。
	have := map[string]bool{}
	for k := range driveHave {
		have[k] = true
	}
	if fnosOK {
		for k := range fnosHave {
			have[k] = true
		}
	}
	for k := range olHave {
		have[k] = true
	}

	// 推送记录核对：上次推送各集的实际保存目录里若仍有视频文件，无论
	// 文件名能否解析出集数（2dland 会用种子内部文件名落盘，如
	// [GM-Team]...[2026][15]，与磁力名标注的 191 集对不上），都视为
	// 该集仍存在；目录已不在或无视频（用户已删除）则移除记录，允许重推。
	epVerified := c.verifyEpPaths(ctx, &sub, have)
	epNote := ""
	if epVerified > 0 {
		epNote = fmt.Sprintf("，推送记录核对 %d 集", epVerified)
	}

	// 可疑空结果守卫：本订阅有推送历史时，若历史已推送的集数在所有来源
	// （飞牛/OpenList/2dland，含推送目录实测）中一个都查不到，极可能是
	// 数据源瞬时异常（如 2026-09-17 02:31 三源同时返回 0 导致整季重推），
	// 本轮直接跳过。用户确需全量重推：删除本订阅后重新提交即可重置历史。
	if len(pushedHave) > 0 {
		covered := 0
		for k := range pushedHave {
			if have[k] {
				covered++
			}
		}
		if covered == 0 {
			log.Printf("subscription %s: none of %d pushed eps found in any source, skip this round (suspected transient source outage)", sub.ID, len(pushedHave))
			return mark(fmt.Sprintf("疑似数据源异常：历史已推送的 %d 集在所有来源（飞牛/OpenList/2dland/推送目录实测）中均查不到，本轮已跳过以防整季重推；若确为删除后需全量重推，请删除本订阅后重新提交", len(pushedHave)), true)
		}
	}

	archTitle := c.archiveTitleFor(ctx, &sub)

	type candidate struct {
		item  drive.PushItem
		hash  string
		keys  []string
		maxEp int
	}
	var candidates []candidate
	learnedFixedURL := false
	for _, r := range matched {
		ms, err := c.Site.FetchMagnets(ctx, r.URL)
		if err != nil || len(ms) == 0 {
			continue
		}
		resourceMatchesSeason := false
		for _, m := range ms {
			text := m.Name + " " + m.Desc + " " + magnetDN(m.Magnet)
			season := ExtractSeason(text)
			if season == 0 {
				season = sub.Season
			}
			if season != sub.Season {
				continue
			}
			eps := episodeKeys(text, season)
			if len(eps) == 0 {
				continue
			}
			resourceMatchesSeason = true
			h := MagnetHash(m.Magnet)
			var missing []string
			maxEp := 0
			for _, k := range eps {
				if have[k] {
					continue
				}
				missing = append(missing, k)
				if ep := keyEpisode(k); ep > maxEp {
					maxEp = ep
				}
			}
			if len(missing) > 0 {
				candidates = append(candidates, candidate{item: drive.PushItem{Name: m.Name, Magnet: m.Magnet, Category: sub.Category, Title: archTitle, Season: season}, hash: h, keys: missing, maxEp: maxEp})
			}
		}
		if !fixedUsed && !learnedFixedURL && resourceMatchesSeason && validSixvDetailURL(r.URL, c.Site.Base) {
			c.Store.SetSixvURL(sub.ID, r.URL)
			sub.SixvURL = r.URL
			learnedFixedURL = true
			log.Printf("subscription %s: learned fixed 6v URL %s", sub.ID, r.URL)
		}
	}
	if len(candidates) == 0 {
		pushedCount := len(pushedHave)
		return mark(fmt.Sprintf("无缺集（飞牛影视已有 %d 集，OpenList已有 %d 集，2dland已有/下载中 %d 集%s，本订阅已推送下载 %d 集、到第%d集）", fnosCount, olCount, driveCount, epNote, pushedCount, sub.LastEp), true)
	}
	planned := map[string]bool{}
	var items []drive.PushItem
	var selected []candidate
	dateDir := time.Now().Format("2006-01-02")
	for _, cand := range candidates {
		var keys []string
		for _, k := range cand.keys {
			if !planned[k] {
				planned[k] = true
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		cand.keys = keys
		cand.item.DownloadDate = dateDir
		items = append(items, cand.item)
		selected = append(selected, cand)
		entry.Discovered = append(entry.Discovered, fmt.Sprintf("%s（%s）", cand.item.Name, strings.Join(keys, "、")))
	}
	if len(items) == 0 {
		return mark("无缺集", true)
	}
	limit := cfgNow.SubPushSafetyLimit
	if limit < 1 || limit > 20 {
		limit = 20
	}
	if len(planned) > limit {
		return mark(fmt.Sprintf("检测到 %d 个疑似缺集，超过当前安全上限 %d 集，已停止推送；请核对飞牛影视地址、季数或OpenList目录，或在设置中调整上限（1～20）", len(planned), limit), false)
	}
	pushCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := c.Drive.Push(pushCtx, items)
	if err != nil {
		return mark("推送失败: "+err.Error(), false)
	}
	var hashes, keys []string
	maxEp := 0
	pushedCount := 0
	failedCount := 0
	epPathGroups := map[string][]string{}
	for i, ri := range res.Items {
		if i >= len(selected) {
			continue
		}
		cand := selected[i]
		entry.Pushes = append(entry.Pushes, PushLog{Name: cand.item.Name, Episodes: cand.keys, SavePath: ri.SavePath, OK: ri.OK, Error: ri.Error})
		if !ri.OK {
			failedCount += len(cand.keys)
			continue
		}
		pushedCount += len(cand.keys)
		hashes = append(hashes, cand.hash)
		keys = append(keys, cand.keys...)
		if ri.SavePath != "" {
			epPathGroups[ri.SavePath] = append(epPathGroups[ri.SavePath], cand.keys...)
		}
		if cand.maxEp > maxEp {
			maxEp = cand.maxEp
		}
	}
	for sp, ks := range epPathGroups {
		c.Store.RecordEpPaths(sub.ID, archTitle, sp, ks)
	}
	if len(keys) == 0 {
		return mark(fmt.Sprintf("推送均失败（共 %d 集，未标记已推送，下次检查将自动重试；若为限流错误请稍后再试）：%s", failedCount, firstError(res.Items)), false)
	}
	c.Store.MarkPushed(sub.ID, hashes, keys, maxEp, "")
	result.Pushed = pushedCount
	cumulativePushed := len(pushedHave)
	seenPushed := map[string]bool{}
	for k := range pushedHave {
		seenPushed[k] = true
	}
	for _, k := range keys {
		if !seenPushed[k] {
			seenPushed[k] = true
			cumulativePushed++
		}
	}
	lastEp := sub.LastEp
	if maxEp > lastEp {
		lastEp = maxEp
	}
	failNote := ""
	if failedCount > 0 {
		failNote = fmt.Sprintf("，另有 %d 集推送失败（未标记，下次检查自动重试）", failedCount)
	}
	return mark(fmt.Sprintf("本次推送 %d 集%s（飞牛影视已有 %d 集，OpenList已有 %d 集，2dland已有/下载中 %d 集%s，本订阅累计已推送下载 %d 集、到第%d集）；已下载到日期目录 %s", pushedCount, failNote, fnosCount, olCount, driveCount, epNote, cumulativePushed, lastEp, dateDir), true)
}

// verifyEpPaths 用推送记录核对各集是否仍存在：集数键 → 所有已知保存目录
// （EpPaths 优先，其次检查日志里的历史推送目录，新→旧）。任一目录中仍有
// 视频即视为该集存在（不依赖文件名解析集数）；所有目录都已无视频且查询
// 正常（通常为用户删除）→ 移除 EpPaths 记录，允许重新推送。目录查询失败
// 时保留记录、不判定，避免把网络错误误当成"文件已删除"。
// 返回通过记录确认仍存在的集数。
func (c *Checker) verifyEpPaths(ctx context.Context, sub *Sub, have map[string]bool) int {
	pathsByKey := map[string][]string{}
	var keyOrder []string
	addPath := func(k, p string) {
		if k == "" || p == "" {
			return
		}
		for _, x := range pathsByKey[k] {
			if x == p {
				return
			}
		}
		if _, ok := pathsByKey[k]; !ok {
			keyOrder = append(keyOrder, k)
		}
		pathsByKey[k] = append(pathsByKey[k], p)
	}
	for k, p := range sub.EpPaths {
		addPath(k, p)
	}
	// 日志按新→旧存储，追加后同键的目录也是新目录在前。
	for _, l := range sub.Logs {
		if !l.OK {
			continue
		}
		for _, pl := range l.Pushes {
			if !pl.OK {
				continue
			}
			for _, k := range pl.Episodes {
				addPath(k, pl.SavePath)
			}
		}
	}
	if len(pathsByKey) == 0 {
		return 0
	}
	type verdict struct {
		has, failed bool
	}
	cache := map[string]verdict{}
	probe := func(p string) (bool, bool) {
		if v, ok := cache[p]; ok {
			return v.has, v.failed
		}
		items, err := c.Drive.ListFiles(ctx, p)
		if err != nil {
			log.Printf("subscription %s: verify ep path %s failed: %v", sub.ID, p, err)
			v := verdict{failed: true}
			cache[p] = v
			return false, true
		}
		has := false
		for _, f := range items {
			if f != nil && !f.Dir && videoName(f.Name) {
				has = true
				break
			}
		}
		v := verdict{has: has}
		cache[p] = v
		return has, false
	}
	verified := 0
	var stale []string
	for _, k := range keyOrder {
		if have[k] {
			verified++
			continue
		}
		found, anyFailed := false, false
		for _, p := range pathsByKey[k] {
			has, failed := probe(p)
			if failed {
				anyFailed = true
				continue
			}
			if has {
				found = true
				break
			}
		}
		if found {
			have[k] = true
			verified++
			continue
		}
		if anyFailed {
			// 有目录查询失败，宁可不判定也不误删记录。
			continue
		}
		if _, ok := sub.EpPaths[k]; ok {
			stale = append(stale, k)
		}
	}
	if len(stale) > 0 {
		c.Store.DropEpPaths(sub.ID, stale)
	}
	return verified
}

// archiveTitleFor 返回本订阅应使用的稳定归档标题：优先用已固定的
// ArchiveTitle；否则从最近推送日志的实际保存目录中提取仍然存在的标题
// 目录名（避免 6v 标题漂移把同一部剧的文件分散到多个目录）；都没有则
// 退回订阅标题。
func (c *Checker) archiveTitleFor(ctx context.Context, sub *Sub) string {
	if t := strings.TrimSpace(sub.ArchiveTitle); t != "" {
		return t
	}
	cfgNow := cfg.Config{}
	if c.GetCfg != nil {
		cfgNow = c.GetCfg()
	}
	cat := drive.CategoryName(sub.Category)
	seen := map[string]bool{}
	for _, l := range sub.Logs {
		for _, pl := range l.Pushes {
			seg := titleSegment(pl.SavePath, cat)
			if seg == "" || seen[seg] {
				continue
			}
			seen[seg] = true
			for _, root := range []string{
				join2(join2(cfgNow.BaseDir, cfgNow.OrganizedDir), cat),
				join2(cfgNow.BaseDir, cat),
			} {
				if _, err := c.Drive.ListFiles(ctx, join2(root, seg)); err == nil {
					c.Store.SetArchiveTitle(sub.ID, seg)
					sub.ArchiveTitle = seg
					return seg
				}
			}
		}
	}
	return sub.Title
}

// titleSegment 从保存目录中提取标题段（分类段之后的那一段）。
func titleSegment(savePath, cat string) string {
	parts := strings.Split(strings.Trim(savePath, "/"), "/")
	for i, seg := range parts {
		if seg == cat && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (c *Checker) checkMovie(ctx context.Context, sub Sub, matched []site6v.Resource, entry *CheckLog, mark func(string, bool) CheckResult) CheckResult {
	dateDir := time.Now().Format("2006-01-02")
	for _, r := range matched {
		ms, err := c.Site.FetchMagnets(ctx, r.URL)
		if err != nil || len(ms) == 0 {
			continue
		}
		if validSixvDetailURL(r.URL, c.Site.Base) && r.URL != sub.SixvURL {
			c.Store.SetSixvURL(sub.ID, r.URL)
			sub.SixvURL = r.URL
		}
		for _, m := range ms {
			h := MagnetHash(m.Magnet)
			// 电影订阅尚无按影片核对 2dland 当前文件的路径，继续保留 hash 防重，
			// 避免每轮重复下载同一部电影；本次实时差集修复仅针对剧集订阅。
			if h != "" && sub.Pushed != nil && sub.Pushed[h] {
				continue
			}
			entry.Discovered = append(entry.Discovered, m.Name)
			item := drive.PushItem{Name: m.Name, Magnet: m.Magnet, Category: sub.Category, Title: r.Title, Season: 1, DownloadDate: dateDir}
			res, err := c.Drive.Push(ctx, []drive.PushItem{item})
			if err != nil {
				entry.Pushes = append(entry.Pushes, PushLog{Name: m.Name, OK: false, Error: err.Error()})
				continue
			}
			if len(res.Items) == 0 {
				continue
			}
			ri := res.Items[0]
			entry.Pushes = append(entry.Pushes, PushLog{Name: m.Name, SavePath: ri.SavePath, OK: ri.OK, Error: ri.Error})
			if !ri.OK {
				continue
			}
			c.Store.MarkPushed(sub.ID, []string{h}, nil, 0, "completed")
			return mark("电影已推送到日期目录 "+dateDir, true)
		}
	}
	return mark("找到资源但暂无可推送磁力，或已推送过", true)
}

func validSixvDetailURL(rawURL, siteBase string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(siteBase))
	if err != nil || base.Hostname() == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".html")
}

func resourceMatches(keyword, title string) bool {
	keyword = strings.TrimSpace(keyword)
	title = strings.TrimSpace(title)
	return keyword != "" && title != "" && (strings.Contains(title, "《"+keyword+"》") || strings.Contains(title, keyword))
}

// firstError 返回第一条推送错误信息，便于判断是否限流。
func firstError(items []drive.PushResultItem) string {
	for _, ri := range items {
		if !ri.OK && ri.Error != "" {
			return ri.Error
		}
	}
	return ""
}
func magnetDN(m string) string {
	u, err := url.Parse(m)
	if err != nil {
		return ""
	}
	return u.Query().Get("dn")
}
func keyEpisode(k string) int { var s, e int; _, _ = fmt.Sscanf(k, "%dx%d", &s, &e); return e }
func epKey(s, e int) string   { return fmt.Sprintf("%dx%d", s, e) }
func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

var (
	seRangeRe = regexp.MustCompile(`(?i)S(\d{1,2})\s*E(\d{1,3})\s*[-~至—]\s*E?(\d{1,3})`)
	seRe      = regexp.MustCompile(`(?i)S(\d{1,2})\s*E(\d{1,3})`)
	eRangeRe  = regexp.MustCompile(`(?i)\bE[P]?\s*(\d{1,3})\s*[-~至—]\s*E?[P]?\s*(\d{1,3})\b`)
	eRe       = regexp.MustCompile(`(?i)\bE[P]?\s*(\d{1,3})\b`)
	cnRangeRe = regexp.MustCompile(`第?\s*([0-9一二三四五六七八九十百]{1,4})\s*[-~至—]\s*([0-9一二三四五六七八九十百]{1,4})\s*[集话話回]`)
	cnRe      = regexp.MustCompile(`第\s*([0-9一二三四五六七八九十百]{1,4})\s*[集话話回]`)
	totalRe   = regexp.MustCompile(`(?:全|共)\s*([0-9]{1,4})\s*[集话話回]`)
	// 6v 常见：第二季22.2160p / 第2季24.2160p，季名后紧跟集号。
	seasonBareEpRe = regexp.MustCompile(`第\s*(?:[0-9]{1,2}|[一二三四五六七八九十]{1,3})\s*季\s*([0-9]{1,3})(?:[ ._\-]|$)`)
	// 发布组常见：[GM-Team]...第2季...[2026][18][HEVC]，四位年份排除后取集号。
	seasonBracketEpRe = regexp.MustCompile(`第\s*(?:[0-9]{1,2}|[一二三四五六七八九十]{1,3})\s*季[^\r\n]*?\[([0-9]{1,3})\]`)
	// 动漫常见命名："153.2160p...mp4"，文件名/磁力名开头数字就是集数。
	leadingEpRe = regexp.MustCompile(`^\s*([0-9]{1,3})(?:[ ._\-]|$)`)
)

func episodeKeys(text string, season int) []string {
	text = strings.ToLower(text)
	var nums []int
	if m := seRangeRe.FindStringSubmatch(text); m != nil {
		s, _ := atoi(m[1])
		a, _ := atoi(m[2])
		b, _ := atoi(m[3])
		if s > 0 {
			season = s
		}
		for i := a; i <= b && i-a < 300; i++ {
			nums = append(nums, i)
		}
	}
	if len(nums) == 0 {
		if ms := seRe.FindAllStringSubmatch(text, -1); len(ms) > 0 {
			for _, m := range ms {
				s, _ := atoi(m[1])
				e, _ := atoi(m[2])
				if s > 0 {
					season = s
				}
				nums = append(nums, e)
			}
		}
	}
	if len(nums) == 0 {
		if m := eRangeRe.FindStringSubmatch(text); m != nil {
			a, _ := atoi(m[1])
			b, _ := atoi(m[2])
			for i := a; i <= b && i-a < 300; i++ {
				nums = append(nums, i)
			}
		}
	}
	if len(nums) == 0 {
		if ms := eRe.FindAllStringSubmatch(text, -1); len(ms) > 0 {
			for _, m := range ms {
				e, _ := atoi(m[1])
				nums = append(nums, e)
			}
		}
	}
	if len(nums) == 0 {
		if m := cnRangeRe.FindStringSubmatch(text); m != nil {
			a, ao := numberText(m[1])
			b, bo := numberText(m[2])
			if ao && bo {
				for i := a; i <= b && i-a < 300; i++ {
					nums = append(nums, i)
				}
			}
		}
	}
	if len(nums) == 0 {
		if m := cnRe.FindStringSubmatch(text); m != nil {
			if n, ok := numberText(m[1]); ok {
				nums = append(nums, n)
			}
		}
	}
	if len(nums) == 0 {
		if m := totalRe.FindStringSubmatch(text); m != nil {
			if n, ok := numberText(m[1]); ok {
				for i := 1; i <= n && i <= 300; i++ {
					nums = append(nums, i)
				}
			}
		}
	}
	if len(nums) == 0 {
		if m := seasonBareEpRe.FindStringSubmatch(text); m != nil {
			if n, ok := numberText(m[1]); ok && n > 0 {
				nums = append(nums, n)
			}
		}
	}
	if len(nums) == 0 {
		if m := seasonBracketEpRe.FindStringSubmatch(text); m != nil {
			if n, ok := numberText(m[1]); ok && n > 0 {
				nums = append(nums, n)
			}
		}
	}
	// 仅在整个文本开头识别，避免误把 2160p、年份等数字当成集数。
	if len(nums) == 0 {
		if m := leadingEpRe.FindStringSubmatch(strings.TrimSpace(text)); m != nil {
			if n, ok := numberText(m[1]); ok && n > 0 {
				nums = append(nums, n)
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range nums {
		if n <= 0 {
			continue
		}
		k := epKey(season, n)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
func numberText(s string) (int, bool) {
	if n, e := atoi(strings.TrimSpace(s)); e == nil && n > 0 {
		return n, true
	}
	return chineseToInt(strings.TrimSpace(s))
}

func join2(p, n string) string {
	p = normalizeOLPath(p)
	n = normalizeOLPath(n)
	if p == "" || p == "/" {
		return "/" + strings.Trim(n, "/")
	}
	if n == "" || n == "/" {
		return p
	}
	return strings.TrimRight(p, "/") + "/" + strings.Trim(n, "/")
}

// normalizeOLPath 允许用户填写 OpenList 完整 URL，也统一处理 URL 编码路径。
// OpenList /api/fs/list 的 path 参数必须是路径，不能包含 http://host:port。
func isAbsoluteOLInput(p string) bool {
	u, err := url.Parse(strings.TrimSpace(p))
	p = strings.TrimSpace(p)
	return strings.HasPrefix(p, "/") || (err == nil && u.Scheme != "" && u.Host != "")
}

func normalizeOLPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if u, err := url.Parse(p); err == nil && u.Scheme != "" && u.Host != "" {
		p = u.Path
	}
	if p == "" {
		return "/"
	}
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func isDrivePathNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 404") || strings.Contains(msg, "file not found")
}

func (c *Checker) driveHave(ctx context.Context, sub *Sub, cc cfg.Config, have map[string]bool) (int, error) {
	count := 0
	add := func(text string, season int) {
		for _, k := range episodeKeys(text, season) {
			if !have[k] {
				have[k] = true
				count++
			}
		}
	}

	// 先查离线任务列表：任务即使还在下载中，或者已完成但尚未归档/上传，
	// 都不应再次推送同一集。SavePath 用于限定到当前订阅。
	tasks, taskErr := c.Drive.ListTasks(ctx)
	for _, t := range tasks {
		if t == nil {
			continue
		}
		pathText := strings.ToLower(t.SavePath)
		kw := strings.ToLower(sub.Keyword)
		if strings.Contains(pathText, kw) || strings.Contains(strings.ToLower(t.Name), kw) {
			add(t.Name, sub.Season)
		}
	}
	if taskErr != nil {
		return count, fmt.Errorf("查询离线任务失败: %w", taskErr)
	}

	cat := drive.CategoryName(sub.Category)
	roots := []string{join2(cc.BaseDir, cat), join2(join2(cc.BaseDir, cc.OrganizedDir), cat)}
	var firstErr error
	readRoots := 0
	for _, root := range roots {
		fs, err := c.Drive.ListFiles(ctx, root)
		if err != nil {
			// 尚未创建过分类目录时 2dland 返回 404，等价于当前没有文件。
			if isDrivePathNotFound(err) {
				readRoots++
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		readRoots++
		for _, f := range fs {
			if f == nil || !f.Dir || !strings.Contains(strings.ToLower(f.Name), strings.ToLower(sub.Keyword)) {
				continue
			}
			kids, err := c.Drive.ListFiles(ctx, join2(root, f.Name))
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, k := range kids {
				if k == nil {
					continue
				}
				hint := sub.Season
				if k.Dir {
					if x := ExtractSeason(k.Name); x > 0 {
						hint = x
					}
					seasonPath := join2(join2(root, f.Name), k.Name)
					grand, err := c.Drive.ListFiles(ctx, seasonPath)
					if err != nil {
						if firstErr == nil {
							firstErr = err
						}
						continue
					}
					for _, g := range grand {
						if g == nil {
							continue
						}
						if g.Dir {
							// 新目录结构：第N季/YYYY-MM-DD/文件。
							dated, derr := c.Drive.ListFiles(ctx, join2(seasonPath, g.Name))
							if derr != nil {
								if firstErr == nil {
									firstErr = derr
								}
								continue
							}
							for _, d := range dated {
								if d != nil && !d.Dir {
									add(d.Name, hint)
								}
							}
						} else {
							add(g.Name, hint)
						}
					}
				} else if videoName(k.Name) {
					add(k.Name, hint)
				}
			}
		}
	}
	log.Printf("subscription %s: 2dland existing episodes=%d tasks=%d roots_read=%d", sub.ID, count, len(tasks), readRoots)
	if readRoots == 0 {
		return count, fmt.Errorf("查询2dland下载区和已下载区均失败: %w", firstErr)
	}
	return count, nil
}

func (c *Checker) openlistHave(ctx context.Context, ol *openlist.Client, cc cfg.Config, sub *Sub, have map[string]bool) (int, error) {
	base := normalizeOLPath(cc.OpenlistBase)
	if base == "" {
		base = "/"
	}
	dir := strings.TrimSpace(sub.OpenlistDir)
	if dir == "" {
		var ok bool
		dir, ok = c.findOL(ctx, ol, base, sub.Keyword)
		if !ok {
			return 0, nil
		}
	} else {
		parsed := normalizeOLPath(dir)
		if isAbsoluteOLInput(dir) {
			dir = parsed
		} else {
			dir = join2(base, parsed)
		}
	}
	count := 0
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > 4 {
			return nil
		}
		es, err := ol.ListDir(ctx, path)
		if err != nil {
			return err
		}
		hint := sub.Season
		if x := ExtractSeason(path); x > 0 {
			hint = x
		}
		for _, e := range es {
			if e.IsDir {
				if err := walk(join2(path, e.Name), depth+1); err != nil {
					return err
				}
			} else if videoName(e.Name) {
				for _, ep := range episodeKeys(e.Name, hint) {
					if !have[ep] {
						have[ep] = true
						count++
					}
				}
			}
		}
		return nil
	}
	return count, walk(dir, 0)
}
func (c *Checker) findOL(ctx context.Context, ol *openlist.Client, base, kw string) (string, bool) {
	type node struct {
		p string
		d int
	}
	q := []node{{base, 0}}
	budget := 40
	for len(q) > 0 && budget > 0 {
		n := q[0]
		q = q[1:]
		es, err := ol.ListDir(ctx, n.p)
		budget--
		if err != nil {
			continue
		}
		for _, e := range es {
			if !e.IsDir {
				continue
			}
			p := join2(n.p, e.Name)
			if strings.Contains(strings.ToLower(e.Name), strings.ToLower(kw)) {
				return p, true
			}
			if n.d < 3 {
				q = append(q, node{p, n.d + 1})
			}
		}
	}
	return "", false
}
