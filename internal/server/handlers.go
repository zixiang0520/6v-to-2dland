package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"kdocs-baiduyun/internal/cfg"
	"kdocs-baiduyun/internal/kdocs"
)

const scrapingWarning = "直接请求拿不到正文时会调用本机 Chrome/Chromium/Edge；可用 KDOCS_BROWSER 指定浏览器。若文档正文仅绘制在 Canvas 中，当前方案无法提取。"

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(err error) map[string]string { return map[string]string{"error": err.Error()} }
func errStr(msg string) map[string]string { return map[string]string{"error": msg} }

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

func (s *Server) uiLogout(w http.ResponseWriter, _ *http.Request) {
	s.sessionMu.Lock()
	s.sessionToken = ""
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) uiSetup(w http.ResponseWriter, r *http.Request) {
	if s.snapshotConfig().AccessPassword != "" {
		writeJSON(w, http.StatusForbidden, errStr("已初始化，修改请用设置页"))
		return
	}
	var body struct {
		Password string `json:"password"`
		KDocsURL string `json:"kdocs_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	body.Password = strings.TrimSpace(body.Password)
	body.KDocsURL = strings.TrimSpace(body.KDocsURL)
	if body.Password == "" {
		writeJSON(w, http.StatusBadRequest, errStr("请设置访问密码"))
		return
	}
	if body.KDocsURL == "" {
		body.KDocsURL = cfg.DefaultKDocsURL
	}
	if err := kdocs.ValidateURL(body.KDocsURL); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	if err := s.updateConfig(func(c *cfg.Config) {
		c.AccessPassword = body.Password
		c.KDocsURL = body.KDocsURL
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	s.startSession(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type resourcesResponse struct {
	Resources   []kdocs.Resource `json:"resources"`
	Total       int              `json:"total"`
	RefreshedAt *time.Time       `json:"refreshed_at,omitempty"`
	SourceMode  string           `json:"source_mode,omitempty"`
	RefreshErr  string           `json:"refresh_error,omitempty"`
	Warning     string           `json:"warning"`
}

func (s *Server) resourceSnapshot(query string) resourcesResponse {
	s.resourcesMu.RLock()
	all := append([]kdocs.Resource(nil), s.resources...)
	refreshedAt := s.refreshedAt
	mode := s.sourceMode
	refreshErr := s.refreshErr
	s.resourcesMu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]kdocs.Resource, 0, len(all))
	for _, item := range all {
		if query == "" || strings.Contains(strings.ToLower(item.Title), query) {
			filtered = append(filtered, item)
		}
	}
	response := resourcesResponse{
		Resources:  filtered,
		Total:      len(filtered),
		SourceMode: mode,
		RefreshErr: refreshErr,
		Warning:    scrapingWarning,
	}
	if !refreshedAt.IsZero() {
		response.RefreshedAt = &refreshedAt
	}
	return response
}

func (s *Server) refreshResources(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	c := s.snapshotConfig()
	resources, mode, err := s.kdocs.Fetch(ctx, c.KDocsURL)
	s.resourcesMu.Lock()
	defer s.resourcesMu.Unlock()
	if err != nil {
		s.refreshErr = err.Error()
		return err
	}
	s.resources = resources
	s.refreshedAt = time.Now()
	s.sourceMode = mode
	s.refreshErr = ""
	return nil
}

func (s *Server) resourcesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.resourceSnapshot(r.URL.Query().Get("q")))
}

func (s *Server) resourcesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	if err := s.refreshResources(ctx); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":   err.Error(),
			"warning": scrapingWarning,
		})
		return
	}
	writeJSON(w, http.StatusOK, s.resourceSnapshot(""))
}

func (s *Server) settingsGet(w http.ResponseWriter, _ *http.Request) {
	c := s.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"kdocs_url":          c.KDocsURL,
		"has_access_password": c.AccessPassword != "",
		"warning":             scrapingWarning,
	})
}

func (s *Server) settingsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessPassword string `json:"access_password"`
		KDocsURL       string `json:"kdocs_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	body.KDocsURL = strings.TrimSpace(body.KDocsURL)
	body.AccessPassword = strings.TrimSpace(body.AccessPassword)
	if err := kdocs.ValidateURL(body.KDocsURL); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	before := s.snapshotConfig()
	if err := s.updateConfig(func(c *cfg.Config) {
		if body.AccessPassword != "" {
			c.AccessPassword = strings.TrimSpace(body.AccessPassword)
		}
		c.KDocsURL = body.KDocsURL
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp(err))
		return
	}
	urlChanged := before.KDocsURL != body.KDocsURL
	if urlChanged {
		s.resourcesMu.Lock()
		s.resources = nil
		s.refreshedAt = time.Time{}
		s.sourceMode = ""
		s.refreshErr = ""
		s.resourcesMu.Unlock()
	}
	if body.AccessPassword != "" {
		s.startSession(w)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url_changed": urlChanged})
}
