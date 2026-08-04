package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
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
	var body struct{ Password string `json:"password"` }
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

// ---------- 设置 ----------
func (s *Server) settingsGet(w http.ResponseWriter, r *http.Request) {
	c := s.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":           c.ClientID,
		"client_secret":       c.ClientSecret,
		"has_credentials":      c.ClientID != "" && c.ClientSecret != "",
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
