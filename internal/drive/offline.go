package drive

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"

	"6v-to-2dland/internal/tmdb"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

// PushItem 是待推送的一条磁力链及其所属资源信息。
type PushItem struct {
	Name     string `json:"name"`     // 磁力链描述
	Magnet   string `json:"magnet"`   // 磁力链
	Category string `json:"category"` // 6v 分类目录名
	Title    string `json:"title"`    // 资源标题（用于 TMDB 规范化）
}

// PushResultItem 是单条磁力链推送的结果。
type PushResultItem struct {
	Name     string `json:"name"`
	Magnet   string `json:"magnet"`
	Category string `json:"category"`
	Folder   string `json:"folder"`    // 规范化后的标题目录名
	SavePath string `json:"save_path"` // 实际保存路径
	Season   string `json:"season,omitempty"`
	OK       bool   `json:"ok"`
	Identity string `json:"identity,omitempty"`
	Error    string `json:"error,omitempty"`
}

// PushResult 是批量推送的汇总。
type PushResult struct {
	Items []PushResultItem `json:"items"`
}

// Push 按磁力链所属资源建立 分类/标题/[季] 目录并添加离线任务。
func (c *Client) Push(ctx context.Context, items []PushItem) (*PushResult, error) {
	s := c.snap()
	res := &PushResult{}
	cache := map[string]string{} // key -> savePath，避免重复建目录
	log.Printf("Push: start items=%d logged_in=%v baseDir=%q", len(items), c.LoggedIn(), s.baseDir)
	for i, it := range items {
		ri := PushResultItem{Name: it.Name, Magnet: it.Magnet, Category: it.Category}
		log.Printf("Push[%d]: category=%q title=%q name=%q isTV=%v", i, it.Category, it.Title, it.Name, isTVCategory(it.Category))

		titleName := normalizeFolderName(ctx, s.tmdb, it.Title, it.Category)
		ri.Folder = titleName

		seasonName := ""
		key := it.Category + "|" + titleName
		if isTVCategory(it.Category) {
			n := seasonFromMagnet(it.Magnet, it.Name)
			seasonName = "第" + strconv.Itoa(n) + "季"
			ri.Season = seasonName
			key += "|" + seasonName
		}

		savePath, cached := cache[key]
		if !cached {
			sp, err := ensureFolderByCategory(ctx, s, it.Category, titleName, seasonName)
			if err != nil {
				ri.Error = "创建文件夹失败: " + err.Error()
				cache[key] = ""
				res.Items = append(res.Items, ri)
				log.Printf("Push[%d]: ensureFolder failed: %v", i, err)
				continue
			}
			savePath = sp
			cache[key] = savePath
		}
		if savePath == "" {
			ri.Error = "创建文件夹失败"
			res.Items = append(res.Items, ri)
			log.Printf("Push[%d]: empty savePath", i)
			continue
		}
		ri.SavePath = savePath
		log.Printf("Push[%d]: savePath=%q cached=%v", i, savePath, cached)

		task, err := s.offline.Add(ctx, &offline.UserTask{
			Url:      it.Magnet,
			Name:     it.Name,
			SavePath: savePath,
		})
		if err != nil {
			ri.Error = err.Error()
			log.Printf("Push[%d]: offline.Add failed: %v", i, err)
		} else {
			ri.OK = true
			ri.Identity = task.Identity
			log.Printf("Push[%d]: offline.Add ok identity=%s", i, task.Identity)
		}
		res.Items = append(res.Items, ri)
	}
	return res, nil
}

var titleRe = regexp.MustCompile(`(?:([0-9]{4}))?[^\d《]*《([^》]+)》`)

// parseTitle 从 6v 标题中提取名字与年份，如 "2026科幻惊悚《灵魂伴侣》..." -> ("灵魂伴侣","2026")。
func parseTitle(t string) (name, year string) {
	if m := titleRe.FindStringSubmatch(t); m != nil {
		year = m[1]
		name = m[2]
		return
	}
	name = t
	return
}

// normalizeFolderName 用 TMDB 规范化标题为 "名字 (年份)"，失败回退原标题。
func normalizeFolderName(ctx context.Context, t *tmdb.Client, title, category string) string {
	if t != nil {
		name, year := parseTitle(title)
		if name == "" {
			name = title
		}
		mediaType := "movie"
		if isTVCategory(category) {
			mediaType = "tv"
		}
		if r, err := t.Search(ctx, name, mediaType); err == nil && r != nil && r.Title != "" {
			y := tmdb.YearFromDate(r.Date)
			if y == "" {
				y = year
			}
			if y != "" {
				return sanitize(r.Title + " (" + y + ")")
			}
			return sanitize(r.Title)
		}
	}
	return sanitize(title)
}

// ListTasks 列出当前离线下载任务。
func (c *Client) ListTasks(ctx context.Context) ([]*offline.UserTask, error) {
	s := c.snap()
	resp, err := s.offline.List(ctx, &offline.OfflineTaskListRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// DeleteTask 删除单个离线任务（同步到 2dland）。deleteFiles 为 true 时同时删除已下载的文件。
func (c *Client) DeleteTask(ctx context.Context, identity string, deleteFiles bool) error {
	if identity == "" {
		return errEmptyIdentity
	}
	s := c.snap()
	_, err := s.offline.Delete(ctx, &offline.OfflineTaskDeleteRequest{
		Identity:    []string{identity},
		DeleteFiles: deleteFiles,
	})
	return err
}

// ClearCompletedTasks 删除所有已完成任务（status==2），返回删除条数。
// deleteFiles 为 true 时同时删除已下载的文件。
func (c *Client) ClearCompletedTasks(ctx context.Context, deleteFiles bool) (int, error) {
	s := c.snap()
	resp, err := s.offline.List(ctx, &offline.OfflineTaskListRequest{})
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, t := range resp.Tasks {
		if t.Status == TaskStatusCompleted {
			ids = append(ids, t.Identity)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := s.offline.Delete(ctx, &offline.OfflineTaskDeleteRequest{
		Identity:    ids,
		DeleteFiles: deleteFiles,
	}); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// 离线任务状态枚举（与 2dland /v6/offline_task/list 返回一致）。
const (
	TaskStatusWaiting   = 0 // 等待中
	TaskStatusRunning   = 1 // 下载中
	TaskStatusCompleted = 2 // 已完成
	TaskStatusFailed    = 3 // 失败
)

var errEmptyIdentity = fmt.Errorf("identity 不能为空")
