package drive

import (
	"sync"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/tmdb"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/apiclient"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/config"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/oauth"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

const apiHost = "openapi.2dland.cn"

// Client 封装 2dland SDK 客户端与各业务服务。
type Client struct {
	mu       sync.RWMutex
	api      *apiclient.Client
	store    config.ConfigStore
	oauth    *oauth.OAuthService
	offline  *offline.OfflineTaskService
	userfile *userfile.UserFileService
	tmdb     *tmdb.Client
	baseDir  string
	clientID string

	deviceCode  string
	loginExpire time.Time
}

// snapshot 是一次请求期间的一致性快照（指针副本，开销极小）。
// 所有公共方法入口取快照，Update* 方法加写锁替换字段，避免数据竞争。
type snapshot struct {
	api      *apiclient.Client
	oauth    *oauth.OAuthService
	offline  *offline.OfflineTaskService
	userfile *userfile.UserFileService
	tmdb     *tmdb.Client
	store    config.ConfigStore
	baseDir  string
	clientID string
}

func (c *Client) snap() snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return snapshot{
		api: c.api, oauth: c.oauth, offline: c.offline, userfile: c.userfile,
		tmdb: c.tmdb, store: c.store, baseDir: c.baseDir, clientID: c.clientID,
	}
}

// New 创建 drive 客户端，token 从 tokenFile 加载；配置了 TMDB key 时启用规范化。
func New(c *cfg.Config) *Client {
	store := config.NewLocalFileConfigStore(c.TokenFile)
	api := apiclient.NewClient(nil, apiHost, c.ClientID, c.ClientSecret, store,
		apiclient.WithTimeout(30*time.Second),
	)
	cl := &Client{
		api:      api,
		store:    store,
		oauth:    oauth.NewOAuthService(api),
		offline:  offline.NewOfflineTaskService(api),
		userfile: userfile.NewUserFileService(api),
		baseDir:  c.BaseDir,
		clientID: c.ClientID,
	}
	if c.TmdbAPIKey != "" {
		cl.tmdb = tmdb.New(c.TmdbAPIKey, c.TmdbProxy, c.TmdbLang)
	}
	return cl
}

// UpdateTMDB 热更新 TMDB 配置；apiKey 为空则禁用规范化。
func (c *Client) UpdateTMDB(apiKey, proxy, lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if apiKey == "" {
		c.tmdb = nil
		return
	}
	c.tmdb = tmdb.New(apiKey, proxy, lang)
}

// UpdateBaseDir 热更新 2dland 根目录名。
func (c *Client) UpdateBaseDir(baseDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseDir != "" {
		c.baseDir = baseDir
	}
}

// UpdateCredentials 热更新 2dland client_id/secret；旧 token 失效，需重新登录。
func (c *Client) UpdateCredentials(clientID, clientSecret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.store.ClearConfigs()
	c.clientID = clientID
	c.api = apiclient.NewClient(nil, apiHost, clientID, clientSecret, c.store,
		apiclient.WithTimeout(30*time.Second),
	)
	c.oauth = oauth.NewOAuthService(c.api)
	c.offline = offline.NewOfflineTaskService(c.api)
	c.userfile = userfile.NewUserFileService(c.api)
	c.deviceCode = ""
}

// Logout 清除 2dland token（退出 2dland 登录）。
func (c *Client) Logout() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.store.ClearConfigs()
	c.api.AccessToken = ""
	c.deviceCode = ""
}
