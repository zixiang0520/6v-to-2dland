package drive

import (
	"context"
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
	Folder   string `json:"folder"`      // 规范化后的标题目录名
	SavePath string `json:"save_path"`   // 实际保存路径
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
	res := &PushResult{}
	cache := map[string]string{} // key -> savePath，避免重复建目录
	for _, it := range items {
		ri := PushResultItem{Name: it.Name, Magnet: it.Magnet, Category: it.Category}

		titleName := c.normalizeFolderName(ctx, it.Title, it.Category)
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
			sp, err := c.EnsureFolderByCategory(ctx, it.Category, titleName, seasonName)
			if err != nil {
				ri.Error = "创建文件夹失败: " + err.Error()
				cache[key] = ""
				res.Items = append(res.Items, ri)
				continue
			}
			savePath = sp
			cache[key] = savePath
		}
		if savePath == "" {
			ri.Error = "创建文件夹失败"
			res.Items = append(res.Items, ri)
			continue
		}
		ri.SavePath = savePath

		task, err := c.offline.Add(ctx, &offline.UserTask{
			Url:      it.Magnet,
			Name:     it.Name,
			SavePath: savePath,
		})
		if err != nil {
			ri.Error = err.Error()
		} else {
			ri.OK = true
			ri.Identity = task.Identity
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
func (c *Client) normalizeFolderName(ctx context.Context, title, category string) string {
	if c.tmdb != nil {
		name, year := parseTitle(title)
		if name == "" {
			name = title
		}
		mediaType := "movie"
		if isTVCategory(category) {
			mediaType = "tv"
		}
		if r, err := c.tmdb.Search(ctx, name, mediaType); err == nil && r != nil && r.Title != "" {
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
	resp, err := c.offline.List(ctx, &offline.OfflineTaskListRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}
