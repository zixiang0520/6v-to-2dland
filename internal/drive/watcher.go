package drive

import (
	"context"
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

// startWatcher 在 Push 成功后异步轮询任务状态，完成时自动调用 OrganizeTask。
// fire-and-forget：不阻塞 Push；服务重启后未完成的轮询会丢失（用户可手动整理）。
// 完成判定：status==10（主），或 BytesProcessed>=BytesTotal>0（字节级兜底，防 status 值偏差）。
func (c *Client) startWatcher(identity, savePath string) {
	go func() {
		ctx := context.Background()
		deadline := time.Now().Add(watchTimeout)
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		log.Printf("watcher: start identity=%q savePath=%q", identity, savePath)
		for range ticker.C {
			if time.Now().After(deadline) {
				log.Printf("watcher: timeout identity=%q savePath=%q (用户可手动整理)", identity, savePath)
				return
			}
			done, err := c.isTaskCompleted(ctx, identity)
			if err != nil {
				log.Printf("watcher: check failed identity=%q err=%v", identity, err)
				continue
			}
			if done {
				log.Printf("watcher: task completed, organizing savePath=%q", savePath)
				// 下载完成文件可能需要一点时间落盘，稍等再整理
				time.Sleep(5 * time.Second)
				res, err := c.OrganizeTask(ctx, savePath)
				if err != nil {
					log.Printf("watcher: organize failed savePath=%q err=%v", savePath, err)
					return
				}
				log.Printf("watcher: organized savePath=%q deleted=%d renamed=%d skipped=%d",
					savePath, len(res.Deleted), len(res.Renamed), len(res.Skipped))
				return
			}
		}
	}()
}

// isTaskCompleted 查询任务是否已完成。SDK 无 Get 单任务接口，用 List 第一页按 identity 查找。
// 任务不在第一页时返回 (false,nil)，下一轮再查。
func (c *Client) isTaskCompleted(ctx context.Context, identity string) (bool, error) {
	s := c.snap()
	resp, err := s.offline.List(ctx, &offline.OfflineTaskListRequest{
		ListInfo: &model.ScanListRequest{Limit: 50},
	})
	if err != nil {
		return false, err
	}
	for _, t := range resp.Tasks {
		if t.Identity != identity {
			continue
		}
		if t.Status == taskStatusCompleted {
			return true, nil
		}
		// 兜底：字节级完成（BT 做种时文件已下完，整理合理）
		if t.BytesTotal > 0 && t.BytesProcessed >= t.BytesTotal {
			return true, nil
		}
		return false, nil
	}
	return false, nil // 任务不在第一页，下轮再查
}
