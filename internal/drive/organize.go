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

// epRe 从文件名解析集数：匹配 S01E05 / E05 / EP05（不捕获 Sxx 前缀）。
var epRe = regexp.MustCompile(`(?i)(?:S\d{1,2})?E(?:P)?(\d{1,3})\b`)

// epCnRe 匹配中文集数：第5集 / 第05集。
var epCnRe = regexp.MustCompile(`第(\d{1,3})集`)

// seasonNumRe 从「第N季」目录名解析季数。
var seasonNumRe = regexp.MustCompile(`第(\d{1,2})季`)

// yearSuffixRe 匹配标题目录名末尾的「 (年份)」，用于提取纯标题。
var yearSuffixRe = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// OrganizeResult 是整理单个任务下载文件的结果。
type OrganizeResult struct {
	SavePath string         `json:"save_path"` // 整理的目录
	Category string         `json:"category"`  // 6v 分类 code（dy/rj/mj/...）
	IsTV     bool           `json:"is_tv"`     // 是否剧集类
	TitleDir string         `json:"title_dir"` // 标题目录名（如「灵魂伴侣 (2026)」）
	Season   int            `json:"season"`    // 季数（剧集）
	Deleted  []string       `json:"deleted"`   // 被删除的广告/杂项文件名
	Renamed  []RenameRecord `json:"renamed"`   // 重命名记录
	Skipped  []string       `json:"skipped"`   // 跳过的视频文件（无法解析集数等）
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
	res := &OrganizeResult{SavePath: savePath}

	// 从 savePath 反推分类 code、标题目录、季数
	cat := categoryFromSavePath(savePath)
	res.Category = cat
	res.IsTV = isTVCategory(cat)
	res.Season = 1

	parts := strings.Split(strings.TrimPrefix(savePath, "/"), "/")
	// parts[0]=baseDir, parts[1]=分类中文名, parts[2]=标题, 剧集 parts[3]=第N季
	if len(parts) >= 3 {
		res.TitleDir = parts[2]
	}
	if res.IsTV && len(parts) >= 4 {
		if m := seasonNumRe.FindStringSubmatch(parts[3]); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				res.Season = n
			}
		}
	}
	log.Printf("OrganizeTask: savePath=%q category=%q isTV=%v titleDir=%q season=%d", savePath, cat, res.IsTV, res.TitleDir, res.Season)

	// 递归列出 savePath 下所有文件
	allFiles, err := c.listAllFilesRecursive(ctx, savePath)
	if err != nil {
		return nil, fmt.Errorf("列出文件失败: %w", err)
	}
	if len(allFiles) == 0 {
		log.Printf("OrganizeTask: no files under %q", savePath)
		return res, nil
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

	// 删除广告/杂项文件
	if len(adIDs) > 0 {
		if err := c.DeleteFiles(ctx, adIDs); err != nil {
			log.Printf("OrganizeTask: delete ads failed: %v", err)
		} else {
			res.Deleted = adNames
		}
	}

	// 重命名视频文件
	// 电影：标题 (年份).ext （多视频加序号 -2/-3）
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
			if pureTitle == "" {
				// 没有标题目录名兜底用纯季集格式
				newName = fmt.Sprintf("S%02dE%02d%s", res.Season, ep, ext)
			} else {
				newName = fmt.Sprintf("%sS%02dE%02d%s", pureTitle, res.Season, ep, ext)
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

	// BT 下载常带同名子目录（如「标题.6v电影 地址发布页...」），视频在子目录里。
	// 把视频移动到 savePath 根，再删除空的 BT 子目录，使文件直接落在标题/季目录下。
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
		}
	}

	// 删除 savePath 下的 BT 子目录（视频已移出、广告已删，子目录为 BT 残留）
	topFiles, err := c.ListFiles(ctx, savePath)
	if err == nil {
		var dirIDs []string
		for _, f := range topFiles {
			if f.Dir {
				dirIDs = append(dirIDs, f.Identity)
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

	log.Printf("OrganizeTask: done deleted=%d renamed=%d skipped=%d", len(res.Deleted), len(res.Renamed), len(res.Skipped))
	return res, nil
}

// categoryFromSavePath 从 savePath 的第二级目录名（分类中文名）反查分类 code。
// savePath 格式：/<baseDir>/<分类中文名>/<标题>...
func categoryFromSavePath(savePath string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimRight(savePath, "/"), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	catName := parts[1]
	for code, name := range categoryNames {
		if name == catName {
			return code
		}
	}
	return ""
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
// 优先匹配 S01E05 / E05 / EP05，再匹配中文「第5集」。
func parseEpisode(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	if m := epRe.FindStringSubmatch(base); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := epCnRe.FindStringSubmatch(base); m != nil {
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
