package server

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"6v-to-2dland/internal/drive"
)

// organizeLogMax 是整理记录的保留上限（超出后丢弃最旧的）。按每条记录几 KB 估算，
// 500 条约 1MB 量级，够回溯相当长时间，又不会让 organize_logs.json 无限膨胀。
const organizeLogMax = 500

// OrganizeLog 是一次整理动作的留痕，供整理出错时回溯「原来的题目/原来的文件名」。
// 关键字段：
//   - TaskName：2dland 离线任务名，即 BT 原始任务名——整理把文件改名后最直接的原名线索
//   - SavePath：整理前的原始保存目录（下载区，含标题目录名）
//   - TitleDir：从路径解析出的标题目录名（重命名时用的标题就是它去掉年份后缀）
//   - Renamed：原文件名 → 新文件名的逐条映射（原名在整理后只剩这里能看到）
type OrganizeLog struct {
	ID          string               `json:"id"`
	Time        string               `json:"time"`      // RFC3339
	Trigger     string               `json:"trigger"`   // auto 自动 / manual 手动 / batch 批量
	TaskName    string               `json:"task_name"` // 任务名（BT 原始名），可能为空
	SavePath    string               `json:"save_path"` // 整理前的原始保存路径
	ArchivePath string               `json:"archive_path,omitempty"`
	Category    string               `json:"category,omitempty"` // 6v 分类 code
	IsTV        bool                 `json:"is_tv"`
	TitleDir    string               `json:"title_dir,omitempty"` // 标题目录名（如「灵魂伴侣 (2026)」）
	Season      int                  `json:"season,omitempty"`
	OK          bool                 `json:"ok"`
	Error       string               `json:"error,omitempty"`
	Deleted     []string             `json:"deleted,omitempty"` // 被删的广告/杂项文件名（含 BT 子目录名）
	Renamed     []drive.RenameRecord `json:"renamed,omitempty"` // 重命名映射
	Skipped     []string             `json:"skipped,omitempty"` // 跳过的视频（解析不到集数等）
}

// newOrganizeLog 由一次整理结果构造记录。res 为空（整理整体失败）时只记路径与错误。
// savePath 是调用方传入的原始路径，用于 res 缺失时兜底。
func newOrganizeLog(trigger, taskName, savePath string, res *drive.OrganizeResult, oerr error) *OrganizeLog {
	rec := &OrganizeLog{
		ID:       hex.EncodeToString(randomBytes(8)),
		Time:     time.Now().Format(time.RFC3339),
		Trigger:  trigger,
		TaskName: taskName,
		SavePath: savePath,
		OK:       oerr == nil,
	}
	if oerr != nil {
		rec.Error = oerr.Error()
	}
	if res != nil {
		if res.OriginalSavePath != "" {
			rec.SavePath = res.OriginalSavePath
		} else if res.SavePath != "" {
			rec.SavePath = res.SavePath
		}
		rec.ArchivePath = res.ArchivePath
		rec.Category = res.Category
		rec.IsTV = res.IsTV
		rec.TitleDir = res.TitleDir
		rec.Season = res.Season
		rec.Deleted = res.Deleted
		rec.Renamed = res.Renamed
		rec.Skipped = res.Skipped
	}
	return rec
}

// organizeLogStore 文件持久化的整理记录存储（organize_logs.json 与 token.json 同目录，容器重启不丢）。
// 内存中保持「最新在前」的顺序，写盘时整表落盘（原子替换）。
type organizeLogStore struct {
	mu    sync.RWMutex
	path  string
	items []*OrganizeLog // 最新在前
	max   int            // 保留上限；<=0 时用 organizeLogMax
}

// newOrganizeLogStore 创建存储并从磁盘加载已有记录。
func newOrganizeLogStore(path string) *organizeLogStore {
	st := &organizeLogStore{path: path, max: organizeLogMax}
	b, err := os.ReadFile(path)
	if err == nil {
		var list []*OrganizeLog
		if json.Unmarshal(b, &list) == nil {
			for _, r := range list {
				if r != nil && r.ID != "" {
					st.items = append(st.items, r)
				}
			}
		} else {
			log.Printf("organize log store: parse %s failed", path)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("organize log store: load %s failed: %v", path, err)
	}
	return st
}

// save 原子写盘（调用方已持写锁）。
func (s *organizeLogStore) save() {
	list := s.items
	if list == nil {
		// 空表也落成 []（而不是 null），便于人工核对文件内容。
		list = []*OrganizeLog{}
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("organize log store: marshal failed: %v", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		log.Printf("organize log store: write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("organize log store: rename failed: %v", err)
	}
}

// Add 插入一条记录（置顶），并裁剪到上限后写盘。
func (s *organizeLogStore) Add(rec *OrganizeLog) {
	if rec == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append([]*OrganizeLog{rec}, s.items...)
	limit := s.max
	if limit <= 0 {
		limit = organizeLogMax
	}
	if len(s.items) > limit {
		s.items = s.items[:limit]
	}
	s.save()
}

// List 返回最多 limit 条记录（最新在前）；limit<=0 表示不限。
func (s *organizeLogStore) List(limit int) []*OrganizeLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > 0 && len(s.items) > limit {
		return append([]*OrganizeLog{}, s.items[:limit]...)
	}
	return append([]*OrganizeLog{}, s.items...)
}

// Len 返回当前记录条数。
func (s *organizeLogStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Delete 按 id 删除记录，返回实际删除的条数。
func (s *organizeLogStore) Delete(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			drop[id] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0:0]
	removed := 0
	for _, r := range s.items {
		if drop[r.ID] {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed == 0 {
		return 0
	}
	s.items = kept
	s.save()
	return removed
}

// Clear 清空全部记录（写盘保留空数组，便于人工核对文件）。
func (s *organizeLogStore) Clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	s.items = nil
	s.save()
	return n
}

// _organizeLogs 在 Server.New 里按配置路径初始化（organize_logs.json 与 token.json 同目录）。
var _organizeLogs *organizeLogStore

// ---------- HTTP ----------

// organizeLogsList 返回整理记录（最新在前）。query: limit（默认 200，上限 1000）。
func (s *Server) organizeLogsList(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	items := _organizeLogs.List(limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": _organizeLogs.Len(),
		"max":   organizeLogMax,
	})
}

// organizeLogsDelete 删除指定记录。body: { ids: [...] } 或 { id: "..." }。
func (s *Server) organizeLogsDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	ids := body.IDs
	if body.ID != "" {
		ids = append(ids, body.ID)
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 id/ids"))
		return
	}
	n := _organizeLogs.Delete(ids)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n, "total": _organizeLogs.Len()})
}

// organizeLogsClear 清空全部整理记录（需显式确认，前端弹窗确认后调用）。
func (s *Server) organizeLogsClear(w http.ResponseWriter, r *http.Request) {
	log.Printf("AUDIT: organizeLogsClear remote=%s", r.RemoteAddr)
	n := _organizeLogs.Clear()
	writeJSON(w, http.StatusOK, map[string]any{"cleared": n, "total": 0})
}
