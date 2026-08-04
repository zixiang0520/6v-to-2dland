package drive

import (
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/tmdb"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/apiclient"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/config"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/offline"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/oauth"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

const apiHost = "openapi.2dland.cn"

// Client 封装 2dland SDK 客户端与各业务服务。
type Client struct {
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

// New 创建 drive 客户端，token 从 tokenFile 加载；配置了 TMDB key 时启用规范化。
func New(c *cfg.Config) *Client {
	store := config.NewLocalFileConfigStore(c.TokenFile)
	api := apiclient.NewClient(
		nil, apiHost, c.ClientID, c.ClientSecret, store,
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
