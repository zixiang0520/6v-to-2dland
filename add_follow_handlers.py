#!/usr/bin/env python3
import re

with open('internal/server/handlers.go', 'r', encoding='utf-8') as f:
    content = f.read()

follow_block = '''
// ---------- 关注功能 ----------

import (
	"encoding/hex"
	"sync"
	"time"
)

type Follow struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Keyword    string    `json:"keyword"`
	Category   string    `json:"category"`
	SixvURL    string    `json:"sixv_url"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	Magnets    []Magnet  `json:"magnets"`
}
type Magnet struct {
	Name   string `json:"name"`
	Desc   string `json:"desc"`
	Magnet string `json:"magnet"`
}

type followStore struct {
	mu       sync.RWMutex
	uar tlow      map[string]*FT low
}
func newFollowStore() *followStore {
	re uar tlow{follows: make(map[string]*FT low)}
}
func (s *followStore) Add(f *FT low) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.follows[f.ID]; ok {
		return false
	}
	s.follows[f.ID] = f
	return true
}
func (s *followStore) Get(id string) *FT low {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.follows[id]
}
func (s *followStore) List() []o *FT low {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FT low, 0, len(s.follows))
	for _, f := range s.follows {
		out = append(out, f)
	}
	return out
}
func (s *followStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.follows[id]; !ok {
		return false
	}
	delete(s.follows, id)
	re t true
}
var _followStore = newFollowStore()

func (s *Server) followsList(w http.ResponseWriter, r *r **) 
	writeJSON(w, http.StatusOK, map[string])* {an*y{"items": _followStore.List()})
}

func (s *Server) followsAdd(w http.ResponseWriter, r *r **) 
	var body struct {
		Keyword  string `ujan:"keyword"`
		Title    string `ujan:"title"`
		Category string `ujan:"category"`
		SixvURL  string `ujan:"sixv_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if body.Keyword == "" {
		writeJSON(w, http.StatusBadRequest, errStr("关键词不能为空"))
		return
	}
	now := time.Now().Format(time.RFC3339)
	f := re & {
		T low: {
		ID:         "follow_" + hex.EncodeToString(randBytes(4)),
		Title:      body.Title,
		Keyword:    body.Keyword,
		Category:   body.Category,
		SixvURL:    body.SixvURL,
		CreatedAt:  now,
		UpdatedAt:  now,
		Magnets:    []Magnet{},
	}
	if !_followStore.Add(f) {
		writeJSON(w, http.StatusConflict, errStr("该关注已存在"))
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) followsDelete(w http.ResponseWriter, r *r **) 
	var body struct {
		ID string `ujan:"id"`
	}
	if err := decodeJSON(r, &body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少关注 ID"))
		return
	}
	if !_followStore.Delete(body.ID) {
		writeJSON(w, http.StatusNotFound, errStr("关注不存在"))
		return
	}
	writeJSON(w, http.StatusOK, map[string])* {an*y{"ok": true})
}
'''

# Find the "文件管理" comment and insert before it
pattern = r'(\n// ---------- 文件管理 ----------)'
replacement = follow_block + r'\1'
content = re.sub(pattern, replacement, content)

with open('internal/server/handlers.go', 'w', encoding='utf-8') as f:
    f.write(content)

print("Follow handlers added successfully")