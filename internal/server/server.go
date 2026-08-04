package server

import (
	"embed"
	"io/fs"
	"net/http"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
	"6v-to-2dland/internal/site6v"
)

// Server 持有各客户端并注册路由。
type Server struct {
	cfg   *cfg.Config
	drive *drive.Client
	site  *site6v.Client
	webFS embed.FS
}

// New 创建 Server，webFS 为内嵌的前端静态资源。
func New(c *cfg.Config, webFS embed.FS) *Server {
	return &Server{
		cfg:   c,
		drive: drive.New(c),
		site:  site6v.NewClient(c.SiteBase),
		webFS: webFS,
	}
}

// Routes 返回 HTTP 处理器（API + 前端静态资源）。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("GET /api/auth/poll", s.authPoll)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/magnets", s.magnets)
	mux.HandleFunc("POST /api/push", s.push)
	mux.HandleFunc("GET /api/tasks", s.tasks)

	sub, err := fs.Sub(s.webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}
