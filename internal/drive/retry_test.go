package drive

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

func TestRetryItemFromTaskPaths(t *testing.T) {
	c := &Client{baseDir: "6v下载", organizedDir: "已下载"}
	const magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	tests := []struct {
		name     string
		savePath string
		want     PushItem
	}{
		{
			name:     "普通电影目录并剥离年份",
			savePath: "/6v下载/电影/灵魂伴侣 (2026)",
			want: PushItem{
				Name: "原始任务名", Magnet: magnet, Category: "dy", Title: "灵魂伴侣",
			},
		},
		{
			name:     "已归档剧集保留季数",
			savePath: "/6v下载/已下载/国剧/某剧 (2025)/第二季",
			want: PushItem{
				Name: "原始任务名", Magnet: magnet, Category: "dlz", Title: "某剧", Season: 2,
			},
		},
		{
			name:     "订阅任务保留日期目录",
			savePath: "/6v下载/已下载/国剧/某剧 (2025)/第2季/2026-11-23",
			want: PushItem{
				Name: "原始任务名", Magnet: magnet, Category: "dlz", Title: "某剧", Season: 2, DownloadDate: "2026-11-23",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &offline.UserTask{
				Name: "原始任务名", Url: "  " + magnet + "  ", SavePath: tt.savePath, Status: taskStatusQueueTimeout,
			}
			got, err := c.retryItemFromTask(task)
			if err != nil {
				t.Fatalf("retryItemFromTask() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("retryItemFromTask() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRetryItemFromTaskRejectsInvalidTasks(t *testing.T) {
	c := &Client{baseDir: "6v下载", organizedDir: "已下载"}
	valid := func() *offline.UserTask {
		return &offline.UserTask{
			Name: "任务", Url: "magnet:?xt=urn:btih:abc", SavePath: "/6v下载/电影/片名 (2026)", Status: taskStatusQueueTimeout,
		}
	}
	tests := []struct {
		name    string
		task    *offline.UserTask
		wantErr error
	}{
		{name: "任务不存在", task: nil, wantErr: ErrRetryNotFound},
		{name: "仅允许排队超时", task: func() *offline.UserTask { v := valid(); v.Status = 2; return v }(), wantErr: ErrRetryInvalidState},
		{name: "空磁力", task: func() *offline.UserTask { v := valid(); v.Url = "  "; return v }()},
		{name: "无法识别分类", task: func() *offline.UserTask { v := valid(); v.SavePath = "/其他位置/片名"; return v }()},
		{name: "订阅日期无效", task: func() *offline.UserTask {
			v := valid()
			v.SavePath = "/6v下载/已下载/国剧/某剧/第1季/昨天"
			return v
		}()},
		{name: "额外目录无法识别", task: func() *offline.UserTask {
			v := valid()
			v.SavePath = "/6v下载/已下载/国剧/某剧/第1季/2026-11-23/额外"
			return v
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.retryItemFromTask(tt.task)
			if err == nil {
				t.Fatal("retryItemFromTask() expected error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

func TestRetryLockIsPerIdentity(t *testing.T) {
	c := &Client{}
	if !c.beginRetry("task-a") {
		t.Fatal("first beginRetry(task-a) should succeed")
	}
	if c.beginRetry("task-a") {
		t.Fatal("concurrent beginRetry(task-a) should be rejected")
	}
	if !c.beginRetry("task-b") {
		t.Fatal("beginRetry(task-b) should not be blocked by task-a")
	}
	c.endRetry("task-a")
	if !c.beginRetry("task-a") {
		t.Fatal("beginRetry(task-a) should succeed after endRetry")
	}
	c.endRetry("task-a")
	c.endRetry("task-b")
}

func TestStopWatcherCancelsAndIsIdempotent(t *testing.T) {
	c := &Client{}
	ctx, cancel := context.WithCancel(context.Background())
	c.watchers = map[string]*watcherHandle{"task-a": {cancel: cancel}}

	c.stopWatcher("task-a")
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stopWatcher did not cancel watcher context")
	}
	if _, ok := c.watchers["task-a"]; ok {
		t.Fatal("stopWatcher did not remove watcher handle")
	}
	c.stopWatcher("task-a") // 重复取消不应 panic。
}

func TestUnregisterOldWatcherKeepsReplacement(t *testing.T) {
	c := &Client{}
	oldHandle := &watcherHandle{}
	newHandle := &watcherHandle{}
	c.watchers = map[string]*watcherHandle{"task-a": newHandle}

	c.unregisterWatcher("task-a", oldHandle)
	if got := c.watchers["task-a"]; got != newHandle {
		t.Fatal("old watcher removed its replacement")
	}
	c.unregisterWatcher("task-a", newHandle)
	if _, ok := c.watchers["task-a"]; ok {
		t.Fatal("current watcher was not removed")
	}
}
