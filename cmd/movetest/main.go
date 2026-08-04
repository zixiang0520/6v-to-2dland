// Command movetest 验证修复后的 Move 逻辑稳定性。
//
// 背景：2dland userfile.Move 的 Dest 字段用 Path 会偶发把文件移到无效位置导致丢失，
// 修复后改用 Dest.Identity（先 Get 解析 destPath → identity）。本测试模拟 organize
// 真实场景——在「BT 子目录」和「标题目录」之间反复移动多个项目，验证：
//  1. 每次 Move 后目标目录数量正确（无丢失）
//  2. 源目录清空（无残留）
//  3. 多轮往返后 identity 仍有效
//
// 测试对象用「子目录」模拟视频文件：userfile.Create 可直接建目录（无需上传内容），
// 且 Move API 对文件/目录走同一接口，稳定性表现一致。
//
// 用法：
//
//	./movetest                       # 从当前目录读 config.json + token.json
//	./movetest -config /path/cfg.json
//	./movetest -cleanup              # 仅清理上次测试残留
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

const (
	numItems = 5  // 每轮移动的项目数（模拟 5 个视频文件）
	rounds   = 20 // 往返测试轮数
	settleMs = 400 // 每次 Move 后等待 2dland 后端一致性的毫秒数
)

func main() {
	configPath := flag.String("config", "config.json", "配置文件路径")
	cleanupOnly := flag.Bool("cleanup", false, "仅清理上次测试残留后退出")
	flag.Parse()

	c, err := cfg.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		log.Fatalf("config.json 缺少 client_id/client_secret，请先在 UI 完成初始化")
	}

	client := drive.New(c)
	if !client.LoggedIn() {
		log.Fatalf("未登录 2dland（%s 无有效 token），请先在 UI 登录", c.TokenFile)
	}

	baseDir := strings.Trim(strings.TrimRight(c.BaseDir, "/"), "/")
	if baseDir == "" {
		baseDir = "6v下载"
	}
	testRoot := "/" + baseDir + "/__MoveTest__"
	titleDir := testRoot + "/测试标题 (2026)"
	btDir := titleDir + "/BT子目录"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	log.Printf("已登录 2dland，baseDir=%q", baseDir)
	log.Printf("测试目录: %s", btDir)

	if *cleanupOnly {
		cleanup(ctx, client, baseDir, testRoot)
		return
	}

	// 1. 清理上次残留
	log.Printf("=== [1/5] 清理上次残留 ===")
	cleanup(ctx, client, baseDir, testRoot)

	// 2. 创建测试目录结构：testRoot / titleDir / btDir
	log.Printf("=== [2/5] 创建测试目录结构 ===")
	if err := mkdirp(ctx, client, btDir); err != nil {
		log.Fatalf("创建目录结构失败: %v", err)
	}
	log.Printf("目录结构就绪")

	// 3. 在 BT 子目录下创建 N 个测试子目录（模拟视频文件）
	log.Printf("=== [3/5] 在 BT 子目录下创建 %d 个测试项 ===", numItems)
	for i := 1; i <= numItems; i++ {
		name := fmt.Sprintf("item-%02d", i)
		if _, err := client.Mkdir(ctx, btDir, name); err != nil {
			log.Fatalf("创建 %s 失败: %v", name, err)
		}
	}
	btItems := listDir(ctx, client, btDir)
	if len(btItems) != numItems {
		log.Fatalf("初始 BT 子目录项数=%d，期望 %d", len(btItems), numItems)
	}
	log.Printf("初始状态: BT 子目录 %d 项 ✓", len(btItems))

	// 4. 往返 Move 测试
	log.Printf("=== [4/5] 开始 %d 轮往返 Move 测试（每轮 BT→标题→BT）===", rounds)
	success, fail := 0, 0
	for r := 1; r <= rounds; r++ {
		ok := runRound(ctx, client, titleDir, btDir, r)
		if ok {
			success++
			log.Printf("[轮 %02d] ✓ 往返成功，%d 项完整无损", r, numItems)
		} else {
			fail++
			log.Printf("[轮 %02d] ✗ 本轮失败（见上）", r)
		}
	}

	// 5. 统计 + 清理
	log.Printf("=== [5/5] 测试结果 ===")
	log.Printf("总轮数=%d  成功=%d  失败=%d  每轮项目数=%d", rounds, success, fail, numItems)
	if fail == 0 {
		log.Printf("✅✅✅ Move 逻辑稳定：%d 轮往返共 %d 次移动，零丢失", rounds, rounds*numItems*2)
	} else {
		log.Printf("❌❌❌ Move 逻辑不稳定：%d/%d 轮失败，请检查", fail, rounds)
	}

	log.Printf("=== 清理测试目录 ===")
	cleanup(ctx, client, baseDir, testRoot)

	if fail > 0 {
		os.Exit(1)
	}
}

// runRound 执行一轮往返：BT 子目录 → 标题目录 → BT 子目录，返回是否成功。
func runRound(ctx context.Context, client *drive.Client, titleDir, btDir string, round int) bool {
	const btName = "BT子目录"

	// ---- BT → 标题目录 ----
	btItems := listDir(ctx, client, btDir)
	if len(btItems) != numItems {
		log.Printf("  [轮 %02d] 移动前 BT 项数=%d（期望 %d），可能上次残留", round, len(btItems), numItems)
		return false
	}
	ids := identities(btItems)
	if err := client.Move(ctx, ids, titleDir); err != nil {
		log.Printf("  [轮 %02d] BT→标题 Move 失败: %v", round, err)
		return false
	}
	time.Sleep(settleMs * time.Millisecond)

	// 验证：标题目录下应有 numItems 个 item（不含 BT子目录）
	titleItems := listDir(ctx, client, titleDir)
	itemCount := 0
	var itemIDs []string
	for _, f := range titleItems {
		if f.Name != btName {
			itemCount++
			itemIDs = append(itemIDs, f.Identity)
		}
	}
	if itemCount != numItems {
		log.Printf("  [轮 %02d] ✗ BT→标题后 item 数=%d（期望 %d）→ 丢失 %d 项!",
			round, itemCount, numItems, numItems-itemCount)
		return false
	}
	// 同时验证 BT 子目录已清空
	btAfter := listDir(ctx, client, btDir)
	if len(btAfter) != 0 {
		log.Printf("  [轮 %02d] ✗ BT→标题后 BT 子目录仍剩 %d 项（期望 0）", round, len(btAfter))
		return false
	}

	// ---- 标题目录 → BT ----
	if err := client.Move(ctx, itemIDs, btDir); err != nil {
		log.Printf("  [轮 %02d] 标题→BT Move 失败: %v", round, err)
		return false
	}
	time.Sleep(settleMs * time.Millisecond)

	// 验证：BT 子目录下应恢复 numItems 项
	btFinal := listDir(ctx, client, btDir)
	if len(btFinal) != numItems {
		log.Printf("  [轮 %02d] ✗ 标题→BT 后 BT 项数=%d（期望 %d）→ 丢失 %d 项!",
			round, len(btFinal), numItems, numItems-len(btFinal))
		return false
	}
	// 同时验证标题目录下只剩 BT子目录
	titleFinal := listDir(ctx, client, titleDir)
	nonBT := 0
	for _, f := range titleFinal {
		if f.Name != btName {
			nonBT++
		}
	}
	if nonBT != 0 {
		log.Printf("  [轮 %02d] ✗ 标题→BT 后标题目录仍剩 %d 个非 BT 项", round, nonBT)
		return false
	}
	return true
}

// mkdirp 逐层创建目录（已存在则跳过），确保 dirPath 完整存在。
func mkdirp(ctx context.Context, client *drive.Client, dirPath string) error {
	parts := strings.Split(strings.Trim(dirPath, "/"), "/")
	cur := "/"
	for _, p := range parts {
		if p == "" {
			continue
		}
		items, err := client.ListFiles(ctx, cur)
		if err != nil {
			return fmt.Errorf("列出 %q 失败: %w", cur, err)
		}
		found := false
		for _, it := range items {
			if it.Name == p {
				found = true
				break
			}
		}
		if !found {
			if _, err := client.Mkdir(ctx, cur, p); err != nil {
				return fmt.Errorf("在 %q 下创建 %q 失败: %w", cur, p, err)
			}
		}
		cur = strings.TrimRight(cur, "/") + "/" + p
	}
	return nil
}

// listDir 列出 dirPath 下的直接子项，出错返回空切片（并记录日志）。
func listDir(ctx context.Context, client *drive.Client, dirPath string) []*userfile.File {
	items, err := client.ListFiles(ctx, dirPath)
	if err != nil {
		log.Printf("  列出 %q 失败: %v", dirPath, err)
		return []*userfile.File{}
	}
	return items
}

// identities 提取一组文件的 identity。
func identities(files []*userfile.File) []string {
	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.Identity)
	}
	return ids
}

// cleanup 删除测试根目录（移到回收站，可恢复）。
func cleanup(ctx context.Context, client *drive.Client, baseDir, testRoot string) {
	parent := "/" + baseDir
	name := path.Base(testRoot)
	items, err := client.ListFiles(ctx, parent)
	if err != nil {
		log.Printf("cleanup: 列出 %q 失败: %v", parent, err)
		return
	}
	for _, it := range items {
		if it.Name == name {
			if err := client.DeleteFiles(ctx, []string{it.Identity}); err != nil {
				log.Printf("cleanup: 删除 %q 失败: %v", it.Path, err)
			} else {
				log.Printf("cleanup: 已删除 %q（移到回收站）", it.Path)
			}
			return
		}
	}
	log.Printf("cleanup: %q 不存在，无需清理", testRoot)
}
