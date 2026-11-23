// 一次性诊断工具：提交一个公开的小磁力，轮询它在 2dland 侧的状态迁移，
// 用来确定 status 10/100/… 的真实含义，结束后自动删除任务和文件。
package main

import (
	"context"
	"fmt"
	"time"

	"6v-to-2dland/internal/cfg"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/apiclient"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/config"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
)

func main() {
	c, err := cfg.Load("data/config.json")
	if err != nil {
		panic(err)
	}
	store := config.NewLocalFileConfigStore("data/token.json")
	api := apiclient.NewClient(nil, "openapi.2dland.cn", c.ClientID, c.ClientSecret, store,
		apiclient.WithTimeout(30*time.Second))
	svc := offline.NewOfflineTaskService(api)
	ctx := context.Background()

	// Big Buck Bunny（Blender 开源短片，WebTorrent 官方示例种子）
	magnet := "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny"
	task, err := svc.Add(ctx, &offline.UserTask{
		Url:      magnet,
		Name:     "zz-status-probe",
		SavePath: "/6v下载/_probe",
	})
	if err != nil {
		fmt.Println("add failed:", err)
		return
	}
	fmt.Printf("added identity=%s status=%d\n", task.Identity, task.Status)

	for i := 0; i < 24; i++ { // 最多 2 分钟
		time.Sleep(5 * time.Second)
		list, err := svc.List(ctx, &offline.OfflineTaskListRequest{})
		if err != nil {
			fmt.Println("list error:", err)
			continue
		}
		for _, t := range list.Tasks {
			if t.Identity != task.Identity {
				continue
			}
			fmt.Printf("t+%02ds status=%-5d progress=%-4d total=%-12d code=%d msg=%q\n",
				(i+1)*5, t.Status, t.Progress, t.BytesTotal, t.Code, t.Message)
		}
	}

	if _, err := svc.Delete(ctx, &offline.OfflineTaskDeleteRequest{
		Identity:    []string{task.Identity},
		DeleteFiles: true,
	}); err != nil {
		fmt.Println("cleanup failed:", err)
	} else {
		fmt.Println("cleaned up")
	}
}
