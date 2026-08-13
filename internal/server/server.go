package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"kdocs-baiduyun/internal/cfg"
	"kdocs-baiduyun/internal/kdocs"
)

type Server struct {
	cfgMu   sync.RWMutex
	cfg     *cfg.Config
	cfgPath string

	kdocs *kdocs.Client
	webFS embed.FS

	sessionMu    sync.RWMutex
	sessionToken string

	resourcesMu sync.RWMutex
	resources   []kdocs.Resource
	refreshedAt time.Time
	sourceMode  string
	refreshErr  string
	refreshMu   sync.Mutex
}

func New(c *cfg.Config, cfgPath string, webFS embed.FS) *Server {
	return &Server{
		cfg:     c,
		cfgPath: cfgPath,
		kdocs:   kdocs.New(),
		webFS:   webFS,
	}
}

func (s *Server) snapshotConfig() cfg.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return *s.cfg
}

func (s *Server) updateConfig(fn func(*cfg.Config)) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	fn(s.cfg)
	return cfg.Save(s.cfgPath, s.cfg)
}

func (s *Server) authenticated(r *http.Request) bool {
	c := s.snapshotConfig()
	if c.AccessPassword == "" {
		return true
	}
	ck, err := r.Cookie("sid")
	if err != nil {
		return false
	}
	s.sessionMu.RLock()
	tok := s.sessionToken
	s.sessionMu.RUnlock()
	return ck.Value != "" && tok != "" && ck.Value == tok
}

func (s *Server) startSession(w http.ResponseWriter) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.sessionMu.Lock()
	s.sessionToken = tok
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
}

func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			writeJSON(w, http.StatusUnauthorized, errStr("未登录或会话已过期，请重新登录"))
			return
		}
		h(w, r)
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/ui/session", s.uiSession)
	mux.HandleFunc("POST /api/ui/login", s.uiLogin)
	mux.HandleFunc("POST /api/ui/logout", s.uiLogout)
	mux.HandleFunc("POST /api/ui/setup", s.uiSetup)
	mux.HandleFunc("GET /api/resources", s.requireAuth(s.resourcesGet))
	mux.HandleFunc("POST /api/resources/refresh", s.requireAuth(s.resourcesRefresh))
	mux.HandleFunc("GET /api/settings", s.requireAuth(s.settingsGet))
	mux.HandleFunc("POST /api/settings", s.requireAuth(s.settingsPost))

	sub, err := fs.Sub(s.webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}
