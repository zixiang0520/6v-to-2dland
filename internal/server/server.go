package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sync"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
	"6v-to-2dland/internal/site6v"
)

// Server 持有各客户端并注册路由。
type Server struct {
	cfgMu   sync.RWMutex
	cfg     *cfg.Config
	cfgPath string

	drive *drive.Client
	site  *site6v.Client
	webFS embed.FS

	mu           sync.RWMutex
	sessionToken string // 单活动会话；登录即替换旧 token
}

// New 创建 Server。cfgPath 用于设置变更时回写 config.json。
func New(c *cfg.Config, cfgPath string, webFS embed.FS) *Server {
	return &Server{
		cfg:     c,
		cfgPath: cfgPath,
		drive:   drive.New(c),
		site:    site6v.NewClient(c.SiteBase),
		webFS:   webFS,
	}
}

// snapshotConfig 返回配置的值副本，供 handler 线程安全读取。
func (s *Server) snapshotConfig() cfg.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return *s.cfg
}

// updateConfig 在锁内修改配置并原子回写 config.json。
func (s *Server) updateConfig(fn func(*cfg.Config)) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	fn(s.cfg)
	return cfg.Save(s.cfgPath, s.cfg)
}

// authenticated 判断当前请求是否已通过 UI 访问登录。
// 未设置访问密码时视为已登录（首次进入走设置向导）。
func (s *Server) authenticated(r *http.Request) bool {
	c := s.snapshotConfig()
	if c.AccessPassword == "" {
		return true
	}
	ck, err := r.Cookie("sid")
	if err != nil {
		return false
	}
	s.mu.RLock()
	tok := s.sessionToken
	s.mu.RUnlock()
	return ck.Value != "" && tok != "" && ck.Value == tok
}

// startSession 生成新会话 token 并写入 HttpOnly cookie。
func (s *Server) startSession(w http.ResponseWriter) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessionToken = tok
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
}

// requireAuth 包裹需要 UI 登录的 handler。
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			writeJSON(w, http.StatusUnauthorized, errStr("未登录或会话已过期，请重新登录"))
			return
		}
		h(w, r)
	}
}

// Routes 返回 HTTP 处理器（API + 前端静态资源）。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// 公共：健康检查 + UI 访问鉴权
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/ui/session", s.uiSession)
	mux.HandleFunc("POST /api/ui/login", s.uiLogin)
	mux.HandleFunc("POST /api/ui/logout", s.uiLogout)
	mux.HandleFunc("POST /api/ui/setup", s.uiSetup)

	// 以下均需 UI 登录
	mux.HandleFunc("GET /api/auth/status", s.requireAuth(s.authStatus))
	mux.HandleFunc("POST /api/auth/login", s.requireAuth(s.authLogin))
	mux.HandleFunc("GET /api/auth/poll", s.requireAuth(s.authPoll))
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.authLogout))
	mux.HandleFunc("GET /api/search", s.requireAuth(s.search))
	mux.HandleFunc("GET /api/home", s.requireAuth(s.home))
	mux.HandleFunc("GET /api/magnets", s.requireAuth(s.magnets))
	mux.HandleFunc("POST /api/push", s.requireAuth(s.push))
	mux.HandleFunc("GET /api/tasks", s.requireAuth(s.tasks))
	mux.HandleFunc("POST /api/tasks/delete", s.requireAuth(s.deleteTask))
	mux.HandleFunc("POST /api/tasks/organize", s.requireAuth(s.taskOrganize))
	// 文件管理（直接管理 2dland 网盘）
	mux.HandleFunc("GET /api/files", s.requireAuth(s.filesList))
	mux.HandleFunc("POST /api/files/mkdir", s.requireAuth(s.filesMkdir))
	mux.HandleFunc("POST /api/files/rename", s.requireAuth(s.filesRename))
	mux.HandleFunc("POST /api/files/move", s.requireAuth(s.filesMove))
	mux.HandleFunc("POST /api/files/delete", s.requireAuth(s.filesDelete))
	mux.HandleFunc("GET /api/files/trash", s.requireAuth(s.filesTrashList))
	mux.HandleFunc("POST /api/files/recover", s.requireAuth(s.filesRecover))
	mux.HandleFunc("GET /api/files/recent", s.requireAuth(s.filesRecent))
	mux.HandleFunc("GET /api/settings", s.requireAuth(s.settingsGet))
	mux.HandleFunc("POST /api/settings", s.requireAuth(s.settingsPost))
	mux.HandleFunc("POST /api/settings/test", s.requireAuth(s.settingsTest))

	sub, err := fs.Sub(s.webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}
