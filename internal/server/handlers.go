package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"6v-to-2dland/internal/drive"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(err error) map[string]string { return map[string]string{"error": err.Error()} }
func errStr(msg string) map[string]string { return map[string]string{"error": msg} }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":       s.drive.LoggedIn(),
		"has_credentials": s.cfg.ClientID != "" && s.cfg.ClientSecret != "",
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

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少参数 q"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	rs := s.site.Search(ctx, q, s.cfg.MaxPages)
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
