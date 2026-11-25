package drive

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/model"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

// taskStatusCompleted 是 2dland 离线任务的「已完成」状态值。
// 2dland 用 status=1000 表示下载完成（实测：progress=100 且 bytes 全部下完时 status=1000）。
const taskStatusCompleted int32 = 1000

// watchInterval 是自动整理轮询任务状态的间隔。
const watchInterval = 20 * time.Second

// watchTimeout 是单次自动整理轮询的最长等待；超时后停止，用户可手动点✨整理。
const watchTimeout = 3 * time.Hour

// OrganizeNotice 是一次整理动作的完整结果通知。成功时 Result 非 nil、失败时 Error 非 nil。
// Trigger 取值 auto（watcher 自动）/ manual（手动点✨）/ batch（批量整理·转移已下载）。
// TaskName 是 2dland 离线任务名，也就是 BT 的原始任务名——整理把文件改名后，
// 这是回溯「原来的题目」最直接的线索。
type OrganizeNotice struct {
	Trigger  string
	TaskName string
	SavePath string // 整理前传入的原始保存路径
	Result   *OrganizeResult
	Error    error
}

type watcherHandle struct {
	cancel context.CancelFunc
}

// startWatcher 在 Push 成功后异步轮询任务状态，完成时自动调用 OrganizeTask。
// fire-and-forget：不阻塞 Push；服务重启后未完成的轮询会丢失（用户可手动整理）。
// 完成判定：status==1000（主），或 BytesProcessed>=BytesTotal>0（字节级兜底，防 status 值偏差）。
// 同一 identity 重复启动时会取消旧 watcher；DeleteTask 成功也会主动取消，避免空轮询。
func (c *Client) startWatcher(identity, savePath string) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &watcherHandle{cancel: cancel}

	c.watcherMu.Lock()
	if c.watchers == nil {
		c.watchers = make(map[string]*watcherHandle)
	}
	old := c.watchers[identity]
	c.watchers[identity] = h
	c.watcherMu.Unlock()
	if old != nil {
		old.cancel()
	}

	go func() {
		defer c.unregisterWatcher(identity, h)
		deadline := time.Now().Add(watchTimeout)
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		log.Printf("watcher: start identity=%q savePath=%q", identity, savePath)
		for {
			select {
			case <-ctx.Done():
				log.Printf("watcher: stopped identity=%q savePath=%q", identity, savePath)
				return
			case <-ticker.C:
			}
			if time.Now().After(deadline) {
				log.Printf("watcher: timeout identity=%q savePath=%q (用户可手动整理)", identity, savePath)
				return
			}
			t, err := c.findTask(ctx, identity)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("watcher: check failed identity=%q err=%v", identity, err)
				continue
			}
			if !taskCompleted(t) {
				continue
			}
			name := ""
			if t != nil {
				name = t.Name
			}
			log.Printf("watcher: task completed, organizing savePath=%q task=%q", savePath, name)
			// 下载完成文件可能需要一点时间落盘，稍等再整理；取消时应立即退出。
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			res, err := c.OrganizeTask(ctx, savePath)
			// 整理结果（含失败）回调上层，落成整理记录；手动/批量整理由 handler 记，不重复。
			c.notifyOrganize(OrganizeNotice{Trigger: "auto", TaskName: name, SavePath: savePath, Result: res, Error: err})
			if err != nil {
				log.Printf("watcher: organize failed savePath=%q err=%v", savePath, err)
				return
			}
			log.Printf("watcher: organized savePath=%q deleted=%d renamed=%d skipped=%d",
				savePath, len(res.Deleted), len(res.Renamed), len(res.Skipped))
			return
		}
	}()
}

func (c *Client) unregisterWatcher(identity string, h *watcherHandle) {
	c.watcherMu.Lock()
	if current := c.watchers[identity]; current == h {
		delete(c.watchers, identity)
	}
	c.watcherMu.Unlock()
}

// stopWatcher 取消并移除任务 watcher；重复调用安全。
func (c *Client) stopWatcher(identity string) {
	c.watcherMu.Lock()
	h := c.watchers[identity]
	if h != nil {
		delete(c.watchers, identity)
	}
	c.watcherMu.Unlock()
	if h != nil {
		h.cancel()
	}
}

// notifyOrganize 触发自动整理回调；未设置回调时忽略。回调内部 panic 不会带崩 watcher goroutine。
func (c *Client) notifyOrganize(n OrganizeNotice) {
	h := c.OnOrganize
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("watcher: OnOrganize panic recovered: %v", r)
		}
	}()
	h(n)
}

// taskCompleted 判断离线任务是否已完成。
// status==1000 为主判定；字节级兜底用于 BT 做种（文件已下完，status 值可能有偏差）。
func taskCompleted(t *offline.UserTask) bool {
	if t == nil {
		return false
	}
	if t.Status == taskStatusCompleted {
		return true
	}
	return t.BytesTotal > 0 && t.BytesProcessed >= t.BytesTotal
}

// findTask 按 identity 查询离线任务。SDK 无 Get 单任务接口，因此沿游标翻页查找，
// 与任务列表保持一致，避免任务位于第 51 条以后时被误判为不存在。
func (c *Client) findTask(ctx context.Context, identity string) (*offline.UserTask, error) {
	s := c.snap()
	const pageSize = 50
	token := ""
	for page := 0; ; page++ {
		if page > 100 { // 与 ListTasks 一致的安全上限，避免异常游标导致无限循环。
			return nil, fmt.Errorf("任务列表分页超过安全上限")
		}
		resp, err := s.offline.List(ctx, &offline.OfflineTaskListRequest{
			ListInfo: &model.ScanListRequest{Limit: pageSize, Token: token},
		})
		if err != nil {
			return nil, err
		}
		for _, t := range resp.Tasks {
			if t != nil && t.Identity == identity {
				return t, nil
			}
		}
		if resp.ListInfo == nil || resp.ListInfo.Token == "" || len(resp.Tasks) == 0 {
			return nil, nil
		}
		token = resp.ListInfo.Token
	}
}
