package drive

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type organizedDisk struct {
	Paths map[string]int64 `json:"paths"` // save_path -> unix
}

var (
	orgMu   sync.Mutex
	orgMemo = map[string]int64{}
	orgFile string
	orgOnce sync.Once
)

func (c *Client) organizedPath() string {
	if c.tokFile == "" {
		return "organized.json"
	}
	return filepath.Join(filepath.Dir(c.tokFile), "organized.json")
}

func (c *Client) loadOrganized() {
	orgOnce.Do(func() {
		orgFile = c.organizedPath()
		b, err := os.ReadFile(orgFile)
		if err != nil {
			return
		}
		var d organizedDisk
		if json.Unmarshal(b, &d) == nil && d.Paths != nil {
			orgMu.Lock()
			orgMemo = d.Paths
			orgMu.Unlock()
		}
	})
}

func (c *Client) isOrganizedPath(savePath string) bool {
	s := c.snap()
	base := "/" + strings.Trim(s.baseDir, "/") + "/"
	org := strings.Trim(s.organizedDir, "/")
	if org == "" {
		org = "已下载"
	}
	prefix := base + org + "/"
	return strings.HasPrefix(strings.TrimRight(savePath, "/")+"/", prefix)
}

func (c *Client) markOrganized(savePath string) {
	c.loadOrganized()
	savePath = strings.TrimRight(savePath, "/")
	if savePath == "" {
		return
	}
	orgMu.Lock()
	orgMemo[savePath] = time.Now().Unix()
	cp := make(map[string]int64, len(orgMemo))
	for k, v := range orgMemo {
		cp[k] = v
	}
	orgMu.Unlock()
	b, _ := json.MarshalIndent(organizedDisk{Paths: cp}, "", "  ")
	_ = os.WriteFile(c.organizedPath(), append(b, '\n'), 0o600)
}

func (c *Client) isOrganized(savePath string) bool {
	c.loadOrganized()
	savePath = strings.TrimRight(savePath, "/")
	orgMu.Lock()
	defer orgMu.Unlock()
	_, ok := orgMemo[savePath]
	return ok
}

func looksOrganizedName(name, titleDir string, isTV bool) bool {
	ext := strings.ToLower(path.Ext(name))
	if !videoExts[ext] {
		return false
	}
	base := strings.TrimSuffix(name, path.Ext(name))
	if isTV {
		return parseEpisode(name) > 0 || fileSeasonRe.MatchString(name)
	}
	if titleDir != "" && (base == titleDir || strings.EqualFold(base, titleDir)) {
		return true
	}
	return yearSuffixRe.MatchString(base)
}

func isJunkName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".txt", ".url", ".html", ".htm", ".torrent", ".exe", ".js", ".lnk":
		return true
	default:
		return false
	}
}

// ProbeOrganized 检查目录里的视频是否已是规范命名、且无广告文件。
func (c *Client) ProbeOrganized(ctx context.Context, savePath string) bool {
	savePath = strings.TrimRight(savePath, "/")
	if savePath == "" {
		return false
	}
	if c.isOrganized(savePath) {
		return true
	}
	files, err := c.listAllFilesRecursive(ctx, savePath)
	if err != nil || len(files) == 0 {
		return false
	}
	parts := c.logicalSaveParts(savePath)
	cat := ""
	if len(parts) >= 1 {
		cat = categoryCodeFromName(parts[0])
	}
	isTV := isTVCategory(cat)
	titleDir := ""
	if len(parts) >= 2 {
		titleDir = parts[1]
	}
	videos, junk, good := 0, 0, 0
	for _, f := range files {
		if f.Dir {
			continue
		}
		if isJunkName(f.Name) {
			junk++
			continue
		}
		ext := strings.ToLower(path.Ext(f.Name))
		if !videoExts[ext] {
			continue
		}
		videos++
		if looksOrganizedName(f.Name, titleDir, isTV) {
			good++
		}
	}
	ok := videos > 0 && junk == 0 && good == videos
	if ok {
		c.markOrganized(savePath)
	}
	return ok
}

// ProbeTasks 给任务列表打 organized 标记（已记录或探测到规范命名）。
func (c *Client) ProbeTasks(ctx context.Context, tasks []*UserTask) {
	for _, t := range tasks {
		if t == nil || t.SavePath == "" {
			continue
		}
		// 整理标记只对当前已完成任务有效；同一保存目录的新下载任务
		// 可能复用旧目录，不能被历史 organized.json 污染。
		if t.Status != taskStatusCompleted {
			t.Organized = false
			continue
		}
		if c.isOrganized(t.SavePath) {
			t.Organized = true
			continue
		}
		if c.ProbeOrganized(ctx, t.SavePath) {
			t.Organized = true
		}
	}
}
