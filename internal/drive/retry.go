package drive

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// RetryTask 重试一个超时/失败任务（典型场景：status=110 取种超时的死任务）：
//  1. 从 2dland 当前任务列表找到该任务，取原磁力与保存路径；
//  2. 删除旧任务记录（安全策略：只删任务，绝不联动删除文件）；
//  3. 按保存路径反推 分类/标题/季，走正常 Push 流程重新提交
//     （自动重建目录 + 挂载 watcher，下载完成后照常自动整理）。
//
// 返回新任务的 PushResultItem；失败返回带原因的 error。
func (c *Client) RetryTask(ctx context.Context, identity string) (*PushResultItem, error) {
	t, err := c.findTask(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	if t == nil {
		return nil, fmt.Errorf("任务不存在或已下线，请刷新任务列表后重试")
	}
	magnet := strings.TrimSpace(t.Url)
	if magnet == "" {
		return nil, fmt.Errorf("任务没有可重试的磁力链接")
	}
	// 删除旧任务记录（保留文件）。删不掉也继续重推——旧的死任务可之后手动清理。
	if delErr := c.DeleteTask(ctx, []string{identity}, false); delErr != nil {
		log.Printf("RetryTask: delete old task %s failed: %v (继续重推)", identity, delErr)
	}
	// 旧任务的 watcher goroutine 无需停止：任务删除后 findTask 查不到，
	// taskCompleted(nil)=false，它会空轮询到超时自然退出，无副作用。

	// 从 savePath 反推推送条目：/<baseDir>/<分类>/<标题>[/第N季]，已归档路径会剥掉「已下载」段。
	item := PushItem{Name: t.Name, Magnet: magnet}
	parts := c.logicalSaveParts(t.SavePath)
	if len(parts) >= 1 {
		item.Category = categoryCodeFromName(parts[0])
	}
	if item.Category == "" {
		return nil, fmt.Errorf("无法从保存路径 %q 反推分类目录，请手动重新推送", t.SavePath)
	}
	if len(parts) >= 2 {
		item.Title = stripYear(parts[1])
	}
	if item.Title == "" {
		item.Title = t.Name
	}
	if len(parts) >= 3 {
		if n, ok := ParseSeasonOK(parts[2]); ok {
			item.Season = n
		}
	}
	log.Printf("RetryTask: re-push category=%q title=%q season=%d magnet=%.60s…", item.Category, item.Title, item.Season, magnet)

	res, err := c.Push(ctx, []PushItem{item})
	if err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		return nil, fmt.Errorf("推送无结果")
	}
	ri := res.Items[0]
	if !ri.OK {
		return &ri, fmt.Errorf("%s", ri.Error)
	}
	log.Printf("RetryTask: ok identity=%s -> new identity=%s savePath=%q", identity, ri.Identity, ri.SavePath)
	return &ri, nil
}
