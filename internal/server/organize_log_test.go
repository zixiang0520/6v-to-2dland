package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"6v-to-2dland/internal/drive"
)

// 整理记录是「整理出错后回溯原标题」的唯一线索，落盘与裁剪行为都要钉住。

func TestOrganizeLogStoreAddKeepsNewestFirst(t *testing.T) {
	st := newOrganizeLogStore(filepath.Join(t.TempDir(), "organize_logs.json"))
	st.Add(&OrganizeLog{ID: "a", Trigger: "auto", SavePath: "/6v下载/电影/A"})
	st.Add(&OrganizeLog{ID: "b", Trigger: "manual", SavePath: "/6v下载/电影/B"})
	st.Add(&OrganizeLog{ID: "c", Trigger: "batch", SavePath: "/6v下载/电影/C"})

	items := st.List(0)
	if len(items) != 3 {
		t.Fatalf("List(0) 应返回 3 条，得到 %d", len(items))
	}
	if items[0].ID != "c" || items[2].ID != "a" {
		t.Fatalf("顺序应为最新在前 c,b,a，得到 %s,%s,%s", items[0].ID, items[1].ID, items[2].ID)
	}
	if got := st.List(2); len(got) != 2 || got[0].ID != "c" {
		t.Fatalf("List(2) 应返回最新两条 c,b，得到 %+v", got)
	}
	if st.Len() != 3 {
		t.Fatalf("Len()=3，得到 %d", st.Len())
	}
}

func TestOrganizeLogStoreTrimsOldestBeyondMax(t *testing.T) {
	st := newOrganizeLogStore(filepath.Join(t.TempDir(), "organize_logs.json"))
	st.max = 3
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		st.Add(&OrganizeLog{ID: id})
	}
	items := st.List(0)
	if len(items) != 3 {
		t.Fatalf("超出上限应裁剪到 3 条，得到 %d", len(items))
	}
	if items[0].ID != "e" || items[1].ID != "d" || items[2].ID != "c" {
		t.Fatalf("应保留最新三条 e,d,c，得到 %s,%s,%s", items[0].ID, items[1].ID, items[2].ID)
	}
}

func TestOrganizeLogStoreDeleteAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "organize_logs.json")
	st := newOrganizeLogStore(path)
	for _, id := range []string{"a", "b", "c"} {
		st.Add(&OrganizeLog{ID: id})
	}
	if n := st.Delete([]string{"a", "missing"}); n != 1 {
		t.Fatalf("Delete 应删除 1 条，得到 %d", n)
	}
	if n := st.Delete(nil); n != 0 {
		t.Fatalf("空 ids 应删除 0 条，得到 %d", n)
	}
	if items := st.List(0); len(items) != 2 || items[0].ID != "c" || items[1].ID != "b" {
		t.Fatalf("删除后应剩 c,b，得到 %+v", items)
	}
	if n := st.Clear(); n != 2 {
		t.Fatalf("Clear 应清空 2 条，得到 %d", n)
	}
	if st.Len() != 0 {
		t.Fatalf("Clear 后 Len 应为 0，得到 %d", st.Len())
	}
	// 清空后仍落盘一个空数组（而不是 null），便于人工核对文件。
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != "[]" {
		t.Fatalf("落盘内容应为 []，得到 %q", got)
	}
	var arr []*OrganizeLog
	if err := json.Unmarshal(b, &arr); err != nil {
		t.Fatalf("落盘内容应为 JSON 数组: %v", err)
	}
	if len(arr) != 0 {
		t.Fatalf("落盘数组应为空，得到 %d 条", len(arr))
	}
	// 清空后应仍可继续写入，且能重新加载。
	st.Add(&OrganizeLog{ID: "after-clear"})
	reloaded := newOrganizeLogStore(path)
	if got := reloaded.List(0); len(got) != 1 || got[0].ID != "after-clear" {
		t.Fatalf("清空后写入应可重新加载，得到 %+v", got)
	}
}

func TestOrganizeLogStorePersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "organize_logs.json")
	st := newOrganizeLogStore(path)
	st.Add(&OrganizeLog{
		ID:          "rec1",
		Time:        "2026-10-21T10:00:00+08:00",
		Trigger:     "auto",
		TaskName:    "[GM-Team][国漫][斗破苍穹][156][AVC][GB]",
		SavePath:    "/6v下载/动漫/斗破苍穹 (2017)/第1季",
		TitleDir:    "斗破苍穹 (2017)",
		Category:    "zydy",
		IsTV:        true,
		Season:      1,
		OK:          true,
		Renamed:     []drive.RenameRecord{{Old: "156.mp4", New: "斗破苍穹S01E156.mp4"}},
		Skipped:     []string{"花絮.mp4"},
		Deleted:     []string{"广告.txt"},
		ArchivePath: "/6v下载/已下载/动漫/斗破苍穹 (2017)/第1季",
	})

	reloaded := newOrganizeLogStore(path)
	items := reloaded.List(0)
	if len(items) != 1 {
		t.Fatalf("重新加载应有 1 条，得到 %d", len(items))
	}
	got := items[0]
	if got.ID != "rec1" || got.TaskName == "" || got.Renamed[0].Old != "156.mp4" || got.Renamed[0].New != "斗破苍穹S01E156.mp4" {
		t.Fatalf("重新加载后字段丢失: %+v", got)
	}
	if got.Renamed == nil || got.Skipped == nil || got.Deleted == nil {
		t.Fatalf("列表字段不应为 nil: %+v", got)
	}
}

func TestOrganizeLogStoreLoadsCorruptedFileAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "organize_logs.json")
	if err := os.WriteFile(path, []byte("{ 这不是 JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	st := newOrganizeLogStore(path)
	if st.Len() != 0 {
		t.Fatalf("损坏文件应加载为空，得到 %d 条", st.Len())
	}
	// 仍应可正常写入
	st.Add(&OrganizeLog{ID: "x"})
	if st.Len() != 1 {
		t.Fatalf("损坏文件后 Add 应可用，得到 %d 条", st.Len())
	}
}

// 记录里必须留下「原来的题目」：任务名 + 原始路径 + 重命名映射。
func TestNewOrganizeLogFromResultKeepsOrigin(t *testing.T) {
	res := &drive.OrganizeResult{
		SavePath:         "/6v下载/已下载/动漫/斗破苍穹 (2017)/第1季",
		OriginalSavePath: "/6v下载/动漫/斗破苍穹 (2017)/第1季",
		ArchivePath:      "/6v下载/已下载/动漫/斗破苍穹 (2017)/第1季",
		Category:         "zydy",
		IsTV:             true,
		TitleDir:         "斗破苍穹 (2017)",
		Season:           1,
		Renamed:          []drive.RenameRecord{{Old: "第156集.mp4", New: "斗破苍穹S01E156.mp4"}},
	}
	rec := newOrganizeLog("manual", "[GM-Team]斗破苍穹 156", "/6v下载/动漫/斗破苍穹 (2017)/第1季", res, nil)
	if !rec.OK || rec.Error != "" {
		t.Fatalf("成功整理应 OK=true 且无 error: %+v", rec)
	}
	if rec.SavePath != "/6v下载/动漫/斗破苍穹 (2017)/第1季" {
		t.Fatalf("SavePath 应记录整理前的原始路径，得到 %q", rec.SavePath)
	}
	if rec.ArchivePath != res.ArchivePath || rec.TitleDir != "斗破苍穹 (2017)" || rec.Season != 1 || !rec.IsTV {
		t.Fatalf("记录字段不完整: %+v", rec)
	}
	if rec.TaskName != "[GM-Team]斗破苍穹 156" {
		t.Fatalf("应保留任务名，得到 %q", rec.TaskName)
	}
	if rec.ID == "" || rec.Time == "" || rec.Trigger != "manual" {
		t.Fatalf("id/time/trigger 应齐全: %+v", rec)
	}
}

// 整理整体失败（res 为 nil）也要留痕，路径与错误不能丢。
func TestNewOrganizeLogFromErrorKeepsPathAndError(t *testing.T) {
	rec := newOrganizeLog("auto", "任务名", "/6v下载/电影/灵魂伴侣 (2026)", nil, errors.New("列出文件失败: 404"))
	if rec.OK {
		t.Fatal("失败整理应 OK=false")
	}
	if rec.Error != "列出文件失败: 404" {
		t.Fatalf("应记录错误信息，得到 %q", rec.Error)
	}
	if rec.SavePath != "/6v下载/电影/灵魂伴侣 (2026)" || rec.TaskName != "任务名" {
		t.Fatalf("失败时仍应记录路径与任务名: %+v", rec)
	}
}
