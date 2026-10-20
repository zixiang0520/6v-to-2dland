package server

import "net/http"

func (s *Server) clearRateLimit(w http.ResponseWriter, r *http.Request) {
	s.drive.ClearRateLimit()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
