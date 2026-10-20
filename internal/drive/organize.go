package drive

import (
	"context"
	"fmt"
	"log"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

// videoExts 是视频文件扩展名白名单，其余扩展名视为广告/杂项文件清理掉。
var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".rmvb": true,
	".ts": true, ".m4v": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".iso": true, ".mpg": true,
	".mpeg": true, ".3gp": true, ".vob": true, ".m2ts": true,
	".mts": true, ".f4v": true, ".rm": true, ".asf": true,
}

// epRe 从文件名解析集数：S01E05 / E05 / EP05。
// 不用 \b：Go RE2 里 \b 对中文边界不可靠；用 (?:^|[^A-Za-z0-9]) 当左边界。
// 明确排除 720p/1080p 这种分辨率，避免 E80 / E20 误匹配。
var epRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(?:S\d{1,2})?E(?:P)?(\d{1,3})(?:[^A-Za-z0-9]|$)`)

// epCnRe 匹配中文集数：第5集 / 第05集 / 第17话 / 更新04。
var epCnRe = regexp.MustCompile(`(?:第(\d{1,3})[集话回]|更新(\d{1,3}))`)

// epRangeRe 合集文件名 S02E01-03：取起始集。
var epRangeRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])S\d{1,2}E(\d{1,3})\s*[-~～]\s*\d{1,3}`)

// fileSeasonRe 从文件名取季数：S02E05 → 2。
var fileSeasonRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])S(\d{1,2})E\d{1,3}`)

// seasonNumRe 从「第N季」目录名解析季数。
var seasonNumRe = regexp.MustCompile(`第(\d{1,2})季`)

// yearSuffixRe 匹配标题目录名末尾的「 (年份)」，用于提取纯标题。
var yearSuffixRe = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// OrganizeResult 是整理单个任务下载文件的结果。
type OrganizeResult struct {
	SavePath    string         `json:"save_path"`    // 整理的目录
	Category    string         `json:"category"`     // 6v 分类 code（dy/rj/mj/...）
	IsTV        bool           `json:"is_tv"`        // 是否剧集类
	TitleDir    string         `json:"title_dir"`    // 标题目录名（如「灵魂伴侣 (2026)」）
	Season      int            `json:"season"`       // 季数（剧集）
	ArchivePath string         `json:"archive_path"` // 下载完成后移动到的已下载目录
	Deleted     []string       `json:"deleted"`      // 被删除的广告/杂项文件名
	Renamed     []RenameRecord `json:"renamed"`      // 重命名记录
	Skipped     []string       `json:"skipped"`      // 跳过的视频文件（无法解析集数等）
}

// RenameRecord 记录一次重命名。
type RenameRecord struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// OrganizeTask 整理一个任务下载目录下的文件：
//   - 删除非视频扩展名的广告/杂项文件（移到回收站，可恢复）
//   - 电影：视频文件重命名为「<标题> (<年份>)」+ 扩展名
//   - 剧集：视频文件重命名为「S01E05」+ 扩展名（季数从「第N季」目录解析，集数从原文件名解析）
//
// savePath 是任务的保存目录，格式：
//
//	/<baseDir>/<分类中文名>/<标题 (年份)>          （电影）
//	/<baseDir>/<分类中文名>/<标题 (年份)>/第N季     （剧集）
//
// 标题目录名与季数从 savePath 反向解析，无需任务推送时记录额外信息。
// 视频文件可能在 BT 子目录里，会递归查找；重命名后保持原位置（不移动）。
func (c *Client) OrganizeTask(ctx context.Context, savePath string) (*OrganizeResult, error) {
	savePath = strings.TrimRight(savePath, "/")
	if savePath == "" {
		savePath = "/"
	}
	originalSavePath := savePath
	res := &OrganizeResult{SavePath: savePath}

	// 从 savePath 反推分类 code、标题目录、季数；兼容 /6v下载/已下载/<分类>/<标题> 归档路径。
	parts := c.logicalSaveParts(savePath)
	cat := ""
	if len(parts) >= 1 {
		cat = categoryCodeFromName(parts[0])
	}
	res.Category = cat
	res.IsTV = isTVCategory(cat)
	res.Season = 1

	// parts[0]=分类中文名, parts[1]=标题, 剧集 parts[2]=第N季
	if len(parts) >= 2 {
		res.TitleDir = parts[1]
	}
	if res.IsTV && len(parts) >= 3 {
		if m := seasonNumRe.FindStringSubmatch(parts[2]); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				res.Season = n
			}
		}
	}
	log.Printf("OrganizeTask: savePath=%q category=%q isTV=%v titleDir=%q season=%d", savePath, cat, res.IsTV, res.TitleDir, res.Season)

	// 手动整理时，任务列表里的 save_path 可能还是下载区旧路径；如果项目已被自动移动到“已下载”，
	// 旧路径会 404。这时自动映射到 /<baseDir>/<已下载>/<分类>/<项目...> 继续整理。
	allFiles, err := c.listAllFilesRecursive(ctx, savePath)
	if err != nil {
		if archived := c.archivedPathFor(savePath); archived != "" {
			if fs2, err2 := c.listAllFilesRecursive(ctx, archived); err2 == nil {
				log.Printf("OrganizeTask: fallback old savePath=%q -> archived=%q", savePath, archived)
				savePath = archived
				res.SavePath = archived
				res.ArchivePath = archived
				allFiles = fs2
				err = nil
			}
		}
		if err != nil {
			return nil, fmt.Errorf("列出文件失败: %w", err)
		}
	}
	// 确认已经有视频文件后，才允许移动到“已下载”。
	// 推送任务会预创建空目录；空目录/未下载完成目录不能提前归档。
	if len(allFiles) == 0 {
		// 旧路径为空但目录仍然存在（不是 404）：可能是同一目录的其它任务先整理时
		// 把文件搬走了，或 watcher 已整体移到“已下载”。此时尝试 fallback 到已下载路径：
		// 若已下载里有视频，说明项目已转移，直接复用已下载路径，让前端能清理残留任务记录。
		if archived := c.archivedPathFor(savePath); archived != "" {
			if fs2, err2 := c.listAllFilesRecursive(ctx, archived); err2 == nil && len(fs2) > 0 {
				log.Printf("OrganizeTask: empty old savePath=%q fallback archived=%q (%d files)", savePath, archived, len(fs2))
				savePath = archived
				res.SavePath = archived
				res.ArchivePath = archived
				allFiles = fs2
			}
		}
		if len(allFiles) == 0 {
			log.Printf("OrganizeTask: no files under %q", savePath)
			return res, nil
		}
	}

	// 分类：视频 vs 广告
	var adIDs, adNames []string
	var videos []*userfile.File
	for _, f := range allFiles {
		if f.Dir {
			continue
		}
		ext := strings.ToLower(path.Ext(f.Name))
		if videoExts[ext] {
			videos = append(videos, f)
		} else {
			adIDs = append(adIDs, f.Identity)
			adNames = append(adNames, f.Name)
		}
	}
	log.Printf("OrganizeTask: total=%d videos=%d ads=%d", len(allFiles), len(videos), len(adIDs))
	if len(videos) == 0 {
		log.Printf("OrganizeTask: no video files yet, skip archive/organize savePath=%q", savePath)
		return res, nil
	}

	// 确认有视频后，先把整个项目目录移动到“已下载”区，再在新位置做删广告、移动视频、重命名。
	// 这样后续操作只围绕最终路径，避免整理后再搬家引起 path/identity 不同步。
	if dest, err := c.archiveOrganizedProject(ctx, savePath); err != nil {
		log.Printf("OrganizeTask: pre-archive project failed savePath=%q err=%v", savePath, err)
		// 同一个 save_path 可能有多个离线任务，或 watcher 与手动整理并发。
		// 若别的调用刚把目录移到“已下载”，这里源路径会 404；不要继续在旧路径上整理，
		// 直接切到已下载路径并重列文件，让前端拿到 archive_path 后可清理任务记录。
		if archived := c.archivedPathFor(savePath); archived != "" {
			if refreshed, err2 := c.listAllFilesRecursive(ctx, archived); err2 == nil {
				log.Printf("OrganizeTask: pre-archive fallback old savePath=%q -> archived=%q", savePath, archived)
				savePath = archived
				res.SavePath = archived
				res.ArchivePath = archived
				allFiles = refreshed
				adIDs, adNames, videos = nil, nil, nil
				for _, f := range allFiles {
					if f.Dir {
						continue
					}
					ext := strings.ToLower(path.Ext(f.Name))
					if videoExts[ext] {
						videos = append(videos, f)
					} else {
						adIDs = append(adIDs, f.Identity)
						adNames = append(adNames, f.Name)
					}
				}
				log.Printf("OrganizeTask: refreshed after pre-archive fallback total=%d videos=%d ads=%d", len(allFiles), len(videos), len(adIDs))
			} else {
				res.Skipped = append(res.Skipped, "归档移动失败: "+err.Error())
			}
		} else {
			res.Skipped = append(res.Skipped, "归档移动失败: "+err.Error())
		}
	} else if dest != "" {
		log.Printf("OrganizeTask: pre-archived project %q -> %q", savePath, dest)
		savePath = dest
		res.SavePath = dest
		res.ArchivePath = dest
		if refreshed, err := c.listAllFilesRecursive(ctx, savePath); err == nil {
			allFiles = refreshed
			adIDs, adNames, videos = nil, nil, nil
			for _, f := range allFiles {
				if f.Dir {
					continue
				}
				ext := strings.ToLower(path.Ext(f.Name))
				if videoExts[ext] {
					videos = append(videos, f)
				} else {
					adIDs = append(adIDs, f.Identity)
					adNames = append(adNames, f.Name)
				}
			}
			log.Printf("OrganizeTask: refreshed after pre-archive total=%d videos=%d ads=%d", len(allFiles), len(videos), len(adIDs))
		} else {
			log.Printf("OrganizeTask: refresh files after pre-archive failed: %v", err)
		}
	}

	// 删除广告/杂项文件
	if len(adIDs) > 0 {
		if err := c.DeleteFiles(ctx, adIDs); err != nil {
			log.Printf("OrganizeTask: delete ads failed: %v", err)
		} else {
			res.Deleted = adNames
		}
	}

	// BT 下载常带同名子目录（如「标题.6v电影 地址发布页...」），视频在子目录里。
	// 先把视频移动到 savePath 根目录，再重新列文件并重命名。
	// 2dland 的 Move/Rename 对同一个 identity 连续操作时偶发最终目录不更新；重新列一遍可拿到最新 identity/path。
	savePathTrim := strings.TrimRight(savePath, "/")
	var moveIDs []string
	for _, v := range videos {
		if path.Dir(v.Path) != savePathTrim {
			moveIDs = append(moveIDs, v.Identity)
		}
	}
	if len(moveIDs) > 0 {
		if err := c.Move(ctx, moveIDs, savePath); err != nil {
			log.Printf("OrganizeTask: move %d videos to %q failed: %v", len(moveIDs), savePath, err)
		} else {
			log.Printf("OrganizeTask: moved %d videos to %q", len(moveIDs), savePath)
			if refreshed, err := c.listAllFilesRecursive(ctx, savePath); err == nil {
				videos = videos[:0]
				for _, f := range refreshed {
					if f.Dir {
						continue
					}
					if videoExts[strings.ToLower(path.Ext(f.Name))] {
						videos = append(videos, f)
					}
				}
			} else {
				log.Printf("OrganizeTask: refresh files after move failed: %v", err)
			}
		}
	}

	// 重命名视频文件
	// 电影：标题 (年份).ext （同一部多盘加序号 -2/-3）
	// 剧集：纯标题S01E05.ext（纯标题 = 去掉「 (年份)」后的标题目录名）
	movieIdx := 0
	pureTitle := stripYear(res.TitleDir) // 剧集用的纯标题
	for _, v := range videos {
		ext := path.Ext(v.Name)
		var newName string
		if res.IsTV {
			ep := parseEpisode(v.Name)
			if ep == 0 {
				// 剧集解析不到集数，跳过不重命名（避免误改名）
				res.Skipped = append(res.Skipped, v.Name)
				continue
			}
			season := res.Season
			if s := parseSeasonFromName(v.Name); s > 0 {
				season = s
			}
			if pureTitle == "" {
				// 没有标题目录名兜底用纯季集格式
				newName = fmt.Sprintf("S%02dE%02d%s", season, ep, ext)
			} else {
				newName = fmt.Sprintf("%sS%02dE%02d%s", pureTitle, season, ep, ext)
			}
		} else {
			if res.TitleDir == "" {
				res.Skipped = append(res.Skipped, v.Name)
				continue
			}
			movieIdx++
			if movieIdx == 1 {
				newName = res.TitleDir + ext
			} else {
				newName = fmt.Sprintf("%s-%d%s", res.TitleDir, movieIdx, ext)
			}
		}
		if newName == v.Name {
			continue
		}
		if err := c.Rename(ctx, v.Identity, newName); err != nil {
			log.Printf("OrganizeTask: rename %q -> %q failed: %v", v.Name, newName, err)
			res.Skipped = append(res.Skipped, v.Name+" (重命名失败)")
			continue
		}
		log.Printf("OrganizeTask: rename %q -> %q", v.Name, newName)
		res.Renamed = append(res.Renamed, RenameRecord{Old: v.Name, New: newName})
	}

	// 删除 savePath 下的 BT 子目录（视频已移出、广告已删，子目录为 BT 残留）。
	// 安全检查：只删除递归确认为空的子目录，防止 Move 失败时把视频一起移到回收站。
	topFiles, err := c.ListFiles(ctx, savePath)
	if err == nil {
		var dirIDs []string
		for _, f := range topFiles {
			if !f.Dir {
				continue
			}
			subs, subErr := c.listAllFilesRecursive(ctx, f.Path)
			if subErr != nil {
				log.Printf("OrganizeTask: check subdir %q failed: %v (skip)", f.Name, subErr)
				continue
			}
			if len(subs) == 0 {
				dirIDs = append(dirIDs, f.Identity)
			} else {
				log.Printf("OrganizeTask: skip non-empty subdir %q (%d files remain, Move may have failed)", f.Name, len(subs))
			}
		}
		if len(dirIDs) > 0 {
			if err := c.DeleteFiles(ctx, dirIDs); err != nil {
				log.Printf("OrganizeTask: delete %d BT subdirs failed: %v", len(dirIDs), err)
			} else {
				log.Printf("OrganizeTask: deleted %d BT subdirs", len(dirIDs))
			}
		}
	}

	// 订阅任务本来就直接位于最终日期目录，archiveOrganizedProject 不会再移动。
	// 此时已经实际读到并整理了视频文件，明确回填 ArchivePath，供前端安全清理任务记录。
	if res.ArchivePath == "" && c.isOrganizedPath(res.SavePath) {
		res.ArchivePath = res.SavePath
	}
	log.Printf("OrganizeTask: done deleted=%d renamed=%d skipped=%d archive=%q", len(res.Deleted), len(res.Renamed), len(res.Skipped), res.ArchivePath)
	c.markOrganized(originalSavePath)
	c.markOrganized(res.SavePath)
	return res, nil
}

// archivedPathFor 把下载区路径映射到已下载区路径。
func (c *Client) archivedPathFor(savePath string) string {
	s := c.snap()
	baseName := sanitize(s.baseDir)
	orgName := sanitize(s.organizedDir)
	if orgName == "" {
		orgName = "已下载"
	}
	basePath := "/" + strings.Trim(baseName, "/")
	sp := strings.TrimRight(savePath, "/")
	if sp == "" || sp == "/" || !strings.HasPrefix(sp, basePath+"/") {
		return ""
	}
	rel := strings.TrimPrefix(sp, basePath+"/")
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] == orgName {
		return ""
	}
	return joinPath(joinPath(basePath, orgName), rel)
}

// archiveOrganizedProject 把下载完成的项目目录移到 /<baseDir>/<organizedDir>/<分类>/<标题...>。
// 电影：/<base>/<分类>/<标题> -> /<base>/<已下载>/<分类>/<标题>
// 剧集：/<base>/<分类>/<标题>/<第N季> -> /<base>/<已下载>/<分类>/<标题>/<第N季>
func (c *Client) archiveOrganizedProject(ctx context.Context, savePath string) (string, error) {
	s := c.snap()
	baseName := sanitize(s.baseDir)
	orgName := sanitize(s.organizedDir)
	if orgName == "" {
		orgName = "已下载"
	}
	basePath := "/" + strings.Trim(baseName, "/")
	sp := strings.TrimRight(savePath, "/")
	if sp == "" || sp == "/" || !strings.HasPrefix(sp, basePath+"/") {
		return "", nil
	}
	rel := strings.TrimPrefix(sp, basePath+"/")
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] == orgName {
		return "", nil
	}

	base, baseID, err := ensureDir(ctx, s.userfile, "/", "", baseName)
	if err != nil {
		return "", err
	}
	org, orgID, err := ensureDir(ctx, s.userfile, base, baseID, orgName)
	if err != nil {
		return "", err
	}
	catPath, catID, err := ensureDir(ctx, s.userfile, org, orgID, parts[0])
	if err != nil {
		return "", err
	}
	destParent := catPath
	if len(parts) >= 3 {
		titlePath, _, err := ensureDir(ctx, s.userfile, catPath, catID, parts[1])
		if err != nil {
			return "", err
		}
		destParent = titlePath
	}
	if strings.TrimRight(destParent, "/") == strings.TrimRight(path.Dir(sp), "/") {
		return "", nil
	}
	src, err := s.userfile.Get(ctx, &userfile.File{Path: sp})
	if err != nil {
		return "", err
	}
	if src == nil || src.Identity == "" {
		return "", fmt.Errorf("项目目录不存在: %s", sp)
	}
	if err := c.Move(ctx, []string{src.Identity}, destParent); err != nil {
		return "", err
	}
	return joinPath(destParent, path.Base(sp)), nil
}

// logicalSaveParts 返回去掉 baseDir 和 organizedDir 后的业务路径段：<分类>/<标题>[/第N季]。
// 兼容：/6v下载/电影/标题 和 /6v下载/已下载/电影/标题。
func (c *Client) logicalSaveParts(savePath string) []string {
	s := c.snap()
	baseName := sanitize(s.baseDir)
	orgName := sanitize(s.organizedDir)
	if orgName == "" {
		orgName = "已下载"
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimRight(savePath, "/"), "/"), "/")
	if len(parts) == 0 {
		return nil
	}
	if parts[0] == baseName {
		parts = parts[1:]
	}
	if len(parts) > 0 && parts[0] == orgName {
		parts = parts[1:]
	}
	return parts
}

func categoryCodeFromName(catName string) string {
	for code, name := range categoryNames {
		if name == catName {
			return code
		}
	}
	return ""
}

// categoryFromSavePath 从 savePath 的第二级目录名（分类中文名）反查分类 code。
// savePath 格式：/<baseDir>/<分类中文名>/<标题>...
func categoryFromSavePath(savePath string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimRight(savePath, "/"), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return categoryCodeFromName(parts[1])
}

// listAllFilesRecursive 递归列出 parentPath 下所有非目录文件（含子目录里的）。
func (c *Client) listAllFilesRecursive(ctx context.Context, parentPath string) ([]*userfile.File, error) {
	files, err := c.ListFiles(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	var all []*userfile.File
	for _, f := range files {
		if f.Dir {
			subs, err := c.listAllFilesRecursive(ctx, f.Path)
			if err != nil {
				log.Printf("listAllFilesRecursive: subdir %q failed: %v", f.Path, err)
				continue
			}
			all = append(all, subs...)
		} else {
			all = append(all, f)
		}
	}
	return all, nil
}

// parseEpisode 从文件名解析集数，解析不到返回 0。
// 优先合集起始集（S02E01-03 → 1），再 S01E05 / E05 / EP05，再「第5集/第17话/更新04」。
func parseEpisode(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	if m := epRangeRe.FindStringSubmatch(base); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := epRe.FindStringSubmatch(base); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := epCnRe.FindStringSubmatch(base); m != nil {
		num := m[1]
		if num == "" {
			num = m[2]
		}
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// parseSeasonFromName 从文件名解析季数（S02E05 → 2），没有则返回 0。
func parseSeasonFromName(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	if m := fileSeasonRe.FindStringSubmatch(base); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// stripYear 从「标题 (年份)」中提取纯标题（去掉末尾的「 (年份)」）。
// 如「灵魂伴侣 (2026)」→「灵魂伴侣」；无年份后缀则原样返回。
func stripYear(titleDir string) string {
	return strings.TrimSpace(yearSuffixRe.ReplaceAllString(titleDir, ""))
}
