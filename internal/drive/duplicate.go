package drive

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"strings"
)

// 本文件实现推送前的重复任务检测：
// 与 2dland 现有离线任务按磁力指纹（btih）比对，命中即让用户确认后再提交。
// 判定用 btih 而不是标题/目录：同一资源的磁力指纹唯一，剧集不同集数指纹不同，
// 不会误伤追更场景；标题比对反而会把同名不同资源的推送挡掉。

// DuplicateHit 是一个与现有任务重复的推送条目。
type DuplicateHit struct {
	Name     string         `json:"name"`     // 待推送资源名（6v 标题）
	Magnet   string         `json:"magnet"`   // 待推送磁力链
	Existing []ExistingTask `json:"existing"` // 已存在的任务（可能多条，如重复提交过）
}

// ExistingTask 是已存在任务的摘要。
type ExistingTask struct {
	Name     string `json:"name"`      // 任务名（BT 原始任务名）
	Status   int32  `json:"status"`    // 2dland 状态码（10/100=排队、2=下载中、1000=已完成…）
	SavePath string `json:"save_path"` // 任务保存目录
	UpdateTs int64  `json:"update_ts"` // 最近更新时间（秒）
}

// MagnetBtih 从磁力链接提取 info-hash 并统一为 40 位小写 hex。
// 支持 urn:btih: 的 hex（40 位）与 base32（32 位）两种写法；
// 解析失败返回空串（无指纹的任务不参与查重，直接放行）。
func MagnetBtih(magnet string) string {
	const prefix = "urn:btih:"
	i := strings.Index(magnet, prefix)
	if i < 0 {
		return ""
	}
	s := magnet[i+len(prefix):]
	if j := strings.IndexAny(s, "&"); j >= 0 {
		s = s[:j]
	}
	s = strings.TrimSpace(s)
	switch len(s) {
	case 40:
		if isHexLower(strings.ToLower(s)) {
			return strings.ToLower(s)
		}
		return ""
	case 32:
		raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(s))
		if err != nil {
			return ""
		}
		return hex.EncodeToString(raw)
	}
	return ""
}

func isHexLower(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// FindDuplicateTasks 把待推送条目与 2dland 现有离线任务按 btih 比对，
// 返回重复命中的条目列表（含已存在任务摘要）；无重复返回空切片。
// 任务列表拉取失败时返回错误——调用方（push handler）选择放行不拦截，
// 查重失败不该挡住正常推送。
func (c *Client) FindDuplicateTasks(ctx context.Context, items []PushItem) ([]DuplicateHit, error) {
	tasks, err := c.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	idx := map[string][]ExistingTask{}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		h := MagnetBtih(t.Url)
		if h == "" {
			continue
		}
		idx[h] = append(idx[h], ExistingTask{
			Name:     t.Name,
			Status:   t.Status,
			SavePath: t.SavePath,
			UpdateTs: t.UpdateTs,
		})
	}
	if len(idx) == 0 {
		return nil, nil
	}
	var hits []DuplicateHit
	seen := map[string]bool{}
	for _, it := range items {
		h := MagnetBtih(it.Magnet)
		if h == "" || seen[h] {
			continue
		}
		if ex, ok := idx[h]; ok {
			seen[h] = true
			hits = append(hits, DuplicateHit{
				Name:     it.Name,
				Magnet:   it.Magnet,
				Existing: ex,
			})
		}
	}
	return hits, nil
}
