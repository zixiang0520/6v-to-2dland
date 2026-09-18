package server

import (
	"context"
	"net/http"
	"time"

	"6v-to-2dland/internal/subscribe"
)

func (s *Server) subscriptionsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.subs.List()})
}

func (s *Server) subscriptionsAdd(w http.ResponseWriter, r *http.Request) {
	var req subscribe.AddReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	sub, err := s.subs.Add(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) subscriptionsDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少订阅 ID"))
		return
	}
	if !s.subs.Delete(body.ID) {
		writeJSON(w, http.StatusNotFound, errStr("订阅不存在"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) subscriptionsUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID                   string `json:"id"`
		Status               string `json:"status"`
		Keyword              string `json:"keyword"`
		Title                string `json:"title"`
		Category             string `json:"category"`
		Type                 string `json:"type"`
		Season               int    `json:"season"`
		OpenlistDir          string `json:"openlist_dir"`
		FnosSeasonURL        string `json:"fnos_season_url"`
		SixvURL              string `json:"sixv_url"`
		CheckIntervalMinutes int    `json:"check_interval_minutes"`
	}
	if err := decodeJSON(r, &body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, errStr("缺少订阅 ID"))
		return
	}
	if body.Status != "" {
		if err := s.subs.SetStatus(body.ID, body.Status); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	sub, err := s.subs.Update(subscribe.UpdateReq{ID: body.ID, Keyword: body.Keyword, Title: body.Title, Category: body.Category, Type: body.Type, Season: body.Season, OpenlistDir: body.OpenlistDir, FnosSeasonURL: body.FnosSeasonURL, SixvURL: body.SixvURL, CheckIntervalMinutes: body.CheckIntervalMinutes})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp(err))
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) subscriptionsCheck(w http.ResponseWriter, r *http.Request) {
	if s.checker == nil {
		writeJSON(w, http.StatusInternalServerError, errStr("检查器未初始化"))
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = decodeJSON(r, &body)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	if body.ID != "" {
		res, err := s.checker.CheckOne(ctx, body.ID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.checker.CheckAll(ctx)})
}
