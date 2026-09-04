package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
	"6v-to-2dland/internal/site6v"
	"6v-to-2dland/internal/tmdb"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(err error) map[string]string { return map[string]string{"error": err.Error()} }
func errStr(msg string) map[string]string { return map[string]string{"error": msg} }

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// ---------- 健康检查 ----------
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- UI 访问鉴权 ----------
func (s *Server) uiSession(w http.ResponseWriter, r *http.Request) {
	c := s.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_required": c.AccessPassword != "",
		"logged_in":     c.AccessPassword == "" || s.authenticated(r),
	})
}

func (s *Server) uiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	c := s.snapshotConfig()
	if c.AccessPassword == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "setup_needed": true})
		return
	}
	if body.Password != c.AccessPassword {
		writeJSON(w, http.StatusUnauthorized, errStr("密码错误"))
		return
	}
	s.startSession(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) uiLogout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.sessionToken = ""
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// uiSetup 仅在未设置访问密码时可用（首次部署引导）。
func (s *Server) uiSetup(w http.ResponseWriter, r *http.Request) {
	// setupMu 串行化首次初始化：消除预检查与写入之间的 TOCTOU 竞态，
	// 并发 setup 请求只有一个能完成初始化。
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	c := s.snapshotConfig()
	if c.AccessPassword != "" {
		writeJSON(w, http.StatusForbidden, errStr("已初始化，修改请用设置页"))
		return
	}
	var body struct {
		Password     string `json:"password"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		TmdbAPIKey   string `json:"tmdb_api_key"`
		TmdbProxy    string `json:"tmdb_proxy"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if body.Password == "" {
		writeJSON(w, http.StatusBadRequest, errStr("请设置访问密码"))
		return
	}
	if body.ClientID == "" || body.ClientSecret == "" {
		writeJSON(w, http.StatusBadRequest, errStr("请填写 2dland client_id / client_secret"))
		return
	}
	if err := s.updateConfig(func(c *cfg.Config) {
		c.AccessPassword = body.Password
		c.ClientID = body.ClientID
		c.ClientSecret = body.ClientSecret
		c.TmdbAPIKey = body.TmdbAPIKey
		c.TmdbProxy = body.TmdbProxy
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	after := s.snapshotConfig()
	s.drive.UpdateCredentials(after.ClientID, after.ClientSecret)
	s.drive.UpdateTMDB(after.TmdbAPIKey, after.TmdbProxy, after.TmdbLang)
	s.startSession(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 2dland 登录 ----------
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	c := s.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":       s.drive.LoggedIn(),
		"has_credentials": c.ClientID != "" && c.ClientSecret != "",
	})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	res, err := s.drive.StartLogin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) authPoll(w http.ResponseWriter, r *http.Request) {
	res, err := s.drive.PollLogin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	s.drive.Logout()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 搜索 / 磁力链 / 推送 / 任务 ----------
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少参数 q"))
		return
	}
	c := s.snapshotConfig()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	rs := s.site.Search(ctx, q, c.MaxPages)
	writeJSON(w, http.StatusOK, rs)
}

func (s *Server) magnets(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if u == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少参数 url"))
		return
	}
	ms, err := s.site.FetchMagnets(r.Context(), u)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Magnets []drive.PushItem `json:"magnets"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if len(body.Magnets) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("未选择磁力链"))
		return
	}
	res, err := s.drive.Push(r.Context(), body.Magnets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.drive.ListTasks(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// deleteTask 删除一个或多个离线任务（同步到 2dland）。
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identities  []string `json:"identities"`
		DeleteFiles bool     `json:"delete_files"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if len(body.Identities) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 identities"))
		return
	}
	if err := s.drive.DeleteTask(r.Context(), body.Identities, body.DeleteFiles); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// taskOrganize 整理一个已完成任务下载的文件：删除广告文件 + 规范化视频文件名。
// 请求体 { save_path } 为任务保存目录，前端任务列表已持有该字段。
func (s *Server) taskOrganize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SavePath string `json:"save_path"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if body.SavePath == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 save_path"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.drive.OrganizeTask(ctx, body.SavePath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- 设置 ----------
func (s *Server) settingsGet(w http.ResponseWriter, r *http.Request) {
	c := s.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":           c.ClientID,
		"client_secret":       c.ClientSecret,
		"has_credentials":     c.ClientID != "" && c.ClientSecret != "",
		"has_access_password": c.AccessPassword != "",
		"tmdb_api_key":        c.TmdbAPIKey,
		"tmdb_proxy":          c.TmdbProxy,
		"tmdb_language":       c.TmdbLang,
		"max_pages":           c.MaxPages,
		"base_dir":            c.BaseDir,
		"logged_in_2dland":    s.drive.LoggedIn(),
	})
}

func (s *Server) settingsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessPassword string `json:"access_password"`
		ClientID       string `json:"client_id"`
		ClientSecret   string `json:"client_secret"`
		TmdbAPIKey     string `json:"tmdb_api_key"`
		TmdbProxy      string `json:"tmdb_proxy"`
		TmdbLanguage   string `json:"tmdb_language"`
		MaxPages       int    `json:"max_pages"`
		BaseDir        string `json:"base_dir"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}

	before := s.snapshotConfig()
	var credsChanged bool
	if err := s.updateConfig(func(c *cfg.Config) {
		if body.AccessPassword != "" {
			c.AccessPassword = body.AccessPassword
		}
		c.ClientID = body.ClientID
		c.ClientSecret = body.ClientSecret
		c.TmdbAPIKey = body.TmdbAPIKey
		c.TmdbProxy = body.TmdbProxy
		if body.TmdbLanguage != "" {
			c.TmdbLang = body.TmdbLanguage
		}
		if body.MaxPages > 0 {
			c.MaxPages = body.MaxPages
		}
		if body.BaseDir != "" {
			c.BaseDir = body.BaseDir
		}
		credsChanged = (c.ClientID != before.ClientID) || (c.ClientSecret != before.ClientSecret)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}

	after := s.snapshotConfig()
	if credsChanged && after.ClientID != "" {
		s.drive.UpdateCredentials(after.ClientID, after.ClientSecret)
	}
	s.drive.UpdateTMDB(after.TmdbAPIKey, after.TmdbProxy, after.TmdbLang)
	s.drive.UpdateBaseDir(after.BaseDir)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"relogin_needed": credsChanged,
	})
}

// settingsTest 用给定（或当前）TMDB 配置做一次真实搜索，验证代理与 Key 是否可用。
func (s *Server) settingsTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TmdbAPIKey   string `json:"tmdb_api_key"`
		TmdbProxy    string `json:"tmdb_proxy"`
		TmdbLanguage string `json:"tmdb_language"`
	}
	_ = decodeJSON(r, &body)
	if body.TmdbAPIKey == "" || body.TmdbProxy == "" || body.TmdbLanguage == "" {
		c := s.snapshotConfig()
		if body.TmdbAPIKey == "" {
			body.TmdbAPIKey = c.TmdbAPIKey
		}
		if body.TmdbProxy == "" {
			body.TmdbProxy = c.TmdbProxy
		}
		if body.TmdbLanguage == "" {
			body.TmdbLanguage = c.TmdbLang
		}
	}
	if body.TmdbAPIKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "未配置 TMDB API Key"})
		return
	}
	start := time.Now()
	cl := tmdb.New(body.TmdbAPIKey, body.TmdbProxy, body.TmdbLanguage)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := cl.Search(ctx, "Inception", "movie")
	dur := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "duration_ms": dur})
		return
	}
	out := map[string]any{"ok": true, "duration_ms": dur}
	if res != nil {
		out["title"] = res.Title
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- 文件管理 ----------

// fileItem 是返回前端的精简文件信息（去掉 SDK File 的冗余字段）。
type fileItem struct {
	Identity string `json:"identity"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	UpdateTs int64  `json:"update_ts"`
	Files    int64  `json:"files"` // 目录内的文件数
	Dirs     int64  `json:"dirs"`  // 目录内的子目录数
}

func (s *Server) filesList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	files, err := s.drive.ListFiles(r.Context(), path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	items := make([]fileItem, 0, len(files))
	for _, f := range files {
		items = append(items, fileItem{
			Identity: f.Identity,
			Name:     f.Name,
			Path:     f.Path,
			Dir:      f.Dir,
			Size:     f.Size,
			UpdateTs: f.UpdateTs,
			Files:    f.Files,
			Dirs:     f.Direcotries,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) filesMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少文件夹名"))
		return
	}
	if body.Parent == "" {
		body.Parent = "/"
	}
	f, err := s.drive.Mkdir(r.Context(), body.Parent, body.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": f.Path})
}

func (s *Server) filesRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identity string `json:"identity"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if body.Identity == "" || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 identity 或 name"))
		return
	}
	if err := s.drive.Rename(r.Context(), body.Identity, body.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) filesMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identities []string `json:"identities"`
		Dest       string   `json:"dest"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if len(body.Identities) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 identities"))
		return
	}
	if body.Dest == "" {
		body.Dest = "/"
	}
	if err := s.drive.Move(r.Context(), body.Identities, body.Dest); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) filesDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identities []string `json:"identities"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if len(body.Identities) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 identities"))
		return
	}
	if err := s.drive.DeleteFiles(r.Context(), body.Identities); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// filesTrashList 列出 2dland 回收站全部文件/目录。
func (s *Server) filesTrashList(w http.ResponseWriter, r *http.Request) {
	files, err := s.drive.ListTrash(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, files)
}

// filesRecover 从回收站恢复文件/目录。
func (s *Server) filesRecover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identities []string `json:"identities"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if len(body.Identities) == 0 {
		writeJSON(w, http.StatusBadRequest, errStr("缺少 identities"))
		return
	}
	if err := s.drive.Recover(r.Context(), body.Identities); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// filesRecent 列出最近更新的文件（全局，用于查找丢失文件）。
func (s *Server) filesRecent(w http.ResponseWriter, r *http.Request) {
	files, err := s.drive.ListRecentFiles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, files)
}

// ---------- 发现页（最新页全抓 + 11 分类近 N 日） ----------
//
// GET /api/home?days=10
// 数据源：/gvod/zx.html、/gvod/dsj.html 各一栏整页全抓；11 个分类各一栏只收近 days 天（默认 10）。
// 返回 {days, cats:[{category,name,items}]}，栏内按发布日降序。

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	days := site6v.CategoryRecentDays
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 31 {
			days = n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	cats, err := s.site.FetchRecent(ctx, days)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days": days,
		"cats": cats,
	})
}
