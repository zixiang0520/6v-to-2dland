package drive

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

const taskStatusQueueTimeout int32 = 110

var (
	ErrRetryNotFound     = errors.New("任务不存在或已下线，请刷新任务列表后重试")
	ErrRetryInProgress   = errors.New("任务正在重试，请勿重复提交")
	ErrRetryInvalidState = errors.New("仅排队超时（status=110）的任务支持重试")
)

// RetryTask 重试一个 status=110 的排队超时任务：
//  1. 从 2dland 当前任务列表找到该任务，并在修改任何任务前完成状态、磁力与路径校验；
//  2. 按原保存路径重建推送条目，保留普通目录或订阅日期目录；
//  3. 成功创建新任务后再删除旧任务记录（只删任务，绝不联动删除文件）。
//
// 同一 identity 的重试串行执行，避免双击或并发请求创建重复任务。若新任务创建成功
// 但旧任务删除失败，会同时返回新任务信息和错误，避免调用方误以为没有产生新任务。
func (c *Client) RetryTask(ctx context.Context, identity string) (*PushResultItem, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil, fmt.Errorf("identity 不能为空")
	}
	if !c.beginRetry(identity) {
		return nil, ErrRetryInProgress
	}
	defer c.endRetry(identity)

	t, err := c.findTask(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	item, err := c.retryItemFromTask(t)
	if err != nil {
		return nil, err
	}
	log.Printf("RetryTask: re-push identity=%s category=%q title=%q season=%d downloadDate=%q",
		identity, item.Category, item.Title, item.Season, item.DownloadDate)

	// 先确保新任务创建成功，再删除旧任务，避免重推失败后原任务记录也丢失。
	res, err := c.Push(ctx, []PushItem{item})
	if err != nil {
		return nil, err
	}
	if res == nil || len(res.Items) == 0 {
		return nil, fmt.Errorf("推送无结果，旧任务未删除")
	}
	ri := res.Items[0]
	if !ri.OK {
		if ri.Error == "" {
			ri.Error = "重新推送失败"
		}
		return &ri, fmt.Errorf("%s（旧任务未删除）", ri.Error)
	}

	if err := c.DeleteTask(ctx, []string{identity}, false); err != nil {
		return &ri, fmt.Errorf("新任务 %s 已创建，但删除旧任务失败，请刷新后手动清理旧任务: %w", ri.Identity, err)
	}
	log.Printf("RetryTask: ok old identity=%s -> new identity=%s savePath=%q", identity, ri.Identity, ri.SavePath)
	return &ri, nil
}

// retryItemFromTask 在修改任务前完整校验并重造推送参数。
func (c *Client) retryItemFromTask(t *offline.UserTask) (PushItem, error) {
	if t == nil {
		return PushItem{}, ErrRetryNotFound
	}
	if t.Status != taskStatusQueueTimeout {
		return PushItem{}, fmt.Errorf("%w，当前状态为 %d", ErrRetryInvalidState, t.Status)
	}
	magnet := strings.TrimSpace(t.Url)
	if magnet == "" {
		return PushItem{}, fmt.Errorf("任务没有可重试的磁力链接，旧任务未删除")
	}

	item := PushItem{Name: t.Name, Magnet: magnet}
	parts := c.logicalSaveParts(t.SavePath)
	if len(parts) >= 1 {
		item.Category = categoryCodeFromName(parts[0])
	}
	if item.Category == "" {
		return PushItem{}, fmt.Errorf("无法从保存路径 %q 反推分类目录，请手动重新推送，旧任务未删除", t.SavePath)
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
	if len(parts) >= 4 {
		if len(parts) > 4 {
			return PushItem{}, fmt.Errorf("保存路径 %q 包含无法识别的额外目录，旧任务未删除", t.SavePath)
		}
		date := strings.TrimSpace(parts[3])
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return PushItem{}, fmt.Errorf("保存路径 %q 中的订阅日期目录无效，旧任务未删除", t.SavePath)
		}
		item.DownloadDate = date
	}
	return item, nil
}

func (c *Client) beginRetry(identity string) bool {
	c.retryMu.Lock()
	defer c.retryMu.Unlock()
	if _, ok := c.retrying[identity]; ok {
		return false
	}
	if c.retrying == nil {
		c.retrying = make(map[string]struct{})
	}
	c.retrying[identity] = struct{}{}
	return true
}

func (c *Client) endRetry(identity string) {
	c.retryMu.Lock()
	delete(c.retrying, identity)
	c.retryMu.Unlock()
}
