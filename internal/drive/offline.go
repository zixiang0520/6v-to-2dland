package drive

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"6v-to-2dland/internal/tmdb"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/model"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

// PushItem 是待推送的一条磁力链及其所属资源信息。
type PushItem struct {
	Name         string `json:"name"`                    // 磁力链描述
	Magnet       string `json:"magnet"`                  // 磁力链
	Category     string `json:"category"`                // 6v 分类目录名
	Title        string `json:"title"`                   // 资源标题（用于 TMDB 规范化）
	Season       int    `json:"season,omitempty"`        // 显式指定季数（0 = 从磁力文本自动解析）
	DownloadDate string `json:"download_date,omitempty"` // 订阅推送日期 YYYY-MM-DD；空 = 普通手动推送目录
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
	// 如果当前处于 2dland 限流冷却期，直接拒绝推送，避免反复触发风控。
	if c.IsRateLimited() {
		return &PushResult{}, fmt.Errorf("2dland 当前处于限流冷却期，请稍后再试（或手动清除限流状态）")
	}
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
		if isTVCategory(it.Category) || it.DownloadDate != "" {
			n := it.Season
			if n <= 0 {
				n = seasonFromMagnet(it.Magnet, it.Name)
			}
			seasonName = "第" + strconv.Itoa(n) + "季"
			ri.Season = seasonName
			key += "|" + seasonName
		}
		if it.DownloadDate != "" {
			key += "|" + it.DownloadDate
		}

		savePath, cached := cache[key]
		if !cached {
			var sp string
			var err error
			if it.DownloadDate != "" {
				sp, err = ensureSubscriptionFolder(ctx, s, it.Category, titleName, seasonName, it.DownloadDate)
			} else {
				sp, err = ensureFolderByCategory(ctx, s, it.Category, titleName, seasonName)
			}
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

		// 2dland 对连续添加任务限流（HTTP 429）。这里带间隔提交 + 限流自动重试，
		// 避免一批任务因限流整批失败；失败的条目不会被标记已推送，下次检查会重试。
		var task *offline.UserTask
		var addErr error
		for attempt := 1; attempt <= 3; attempt++ {
			task, addErr = s.offline.Add(ctx, &offline.UserTask{
				Url:      it.Magnet,
				Name:     it.Name,
				SavePath: savePath,
			})
			if addErr == nil {
				break
			}
			if isRateLimited(addErr) && attempt < 3 {
				wait := time.Duration(attempt*30) * time.Second
				log.Printf("Push[%d]: rate limited, retry %d/3 after %v", i, attempt+1, wait)
				select {
				case <-ctx.Done():
				case <-time.After(wait):
				}
				continue
			}
			break
		}
		if addErr != nil {
			ri.Error = addErr.Error()
			log.Printf("Push[%d]: offline.Add failed: %v", i, addErr)
			// 连续 2 个任务被限流 → 停止本轮推送，避免反复触发风控加重限制。
			if isRateLimited(addErr) {
				c.mu.Lock()
				c.rateLimited = true
				c.rateLimitUntil = time.Now().Add(30 * time.Minute)
				c.mu.Unlock()
			}
		} else {
			ri.OK = true
			ri.Identity = task.Identity
			log.Printf("Push[%d]: offline.Add ok identity=%s", i, task.Identity)
			// 异步轮询任务状态，下载完成后自动整理文件（删广告 + 规范命名）
			c.startWatcher(task.Identity, savePath)
		}
		res.Items = append(res.Items, ri)
		if i < len(items)-1 {
			select {
			case <-ctx.Done():
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}
	return res, nil
}

// isRateLimited 判断 2dland 是否返回限流错误（HTTP 429 / USER_OPERATE_TOO_FREQUENTLY）。
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "TOO_FREQUENTLY")
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

// ListTasks 列出全部离线下载任务。
// 2dland 用游标分页（ListInfo.Token），这里循环拉取每页 50 条直到 token 为空。
func (c *Client) ListTasks(ctx context.Context) ([]*UserTask, error) {
	s := c.snap()
	const pageSize = 50
	var all []*UserTask
	var token string
	for page := 0; ; page++ {
		resp, err := s.offline.List(ctx, &offline.OfflineTaskListRequest{
			ListInfo: &model.ScanListRequest{Limit: pageSize, Token: token},
		})
		if err != nil {
			if page == 0 {
				return nil, err
			}
			log.Printf("ListTasks: page %d error: %v (returning %d tasks so far)", page, err, len(all))
			break
		}
		for _, t := range resp.Tasks {
			if t == nil {
				continue
			}
			all = append(all, &UserTask{
				Identity:       t.Identity,
				Name:           t.Name,
				Url:            t.Url,
				SavePath:       t.SavePath,
				Status:         t.Status,
				Progress:       float64(t.Progress),
				BytesTotal:     t.BytesTotal,
				BytesProcessed: t.BytesProcessed,
			})
		}
		if resp.ListInfo == nil || resp.ListInfo.Token == "" || len(resp.Tasks) == 0 {
			break
		}
		token = resp.ListInfo.Token
		if page > 100 { // 安全上限，避免异常情况下无限循环（5000+ 任务）
			log.Printf("ListTasks: hit page safety cap (%d pages, %d tasks)", page, len(all))
			break
		}
	}
	c.ProbeTasks(ctx, all)
	log.Printf("ListTasks: total %d tasks", len(all))
	return all, nil
}

// DeleteTask 删除一个或多个离线任务（同步到 2dland）。deleteFiles 为 true 时同时删除已下载的文件。
//
// 注意：SDK 的 OfflineTaskDeleteRequest.DeleteFiles 带 json:",omitempty"，
// bool 零值 false 会被序列化时丢弃字段，导致 2dland 服务端使用默认值（删除文件）。
// 这里绕过 SDK 结构体，直接用 map 构造请求体，确保 delete_files=false 显式出现在 JSON 中。
func (c *Client) DeleteTask(ctx context.Context, identities []string, deleteFiles bool) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	s := c.snap()
	body := map[string]any{
		"identity":     identities,
		"delete_files": deleteFiles,
	}
	result := struct {
		Count int64 `json:"count,string"`
	}{}
	err := s.api.Post(ctx, "/v6/offline_task/delete", nil, body, &result)
	log.Printf("DeleteTask: identities=%d delete_files=%v -> count=%d err=%v", len(identities), deleteFiles, result.Count, err)
	return err
}

var errEmptyIdentity = fmt.Errorf("identity 不能为空")
