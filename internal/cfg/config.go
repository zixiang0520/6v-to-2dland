package cfg

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 是应用配置，对应 config.json。
type Config struct {
	Listen       string `json:"listen"`        // HTTP 监听地址，默认 :8080
	BaseDir      string `json:"base_dir"`      // 2dland 网盘内根目录名，默认 6v下载
	MaxPages     int    `json:"max_pages"`     // 每个分类最大翻页数，默认 8
	ClientID     string `json:"client_id"`     // 2dland 开放平台 client_id
	ClientSecret string `json:"client_secret"` // 2dland 开放平台 client_secret
	TokenFile    string `json:"token_file"`    // token 持久化文件，默认 token.json
	SiteBase     string `json:"site_base"`     // 6v520 站点根，默认 http://www.6v520.com

	TmdbAPIKey string `json:"tmdb_api_key"` // TMDB API Key，留空则不规范化
	TmdbProxy  string `json:"tmdb_proxy"`   // 访问 TMDB 的代理，如 http://127.0.0.1:7890
	TmdbLang   string `json:"tmdb_language"` // TMDB 语言，默认 zh-CN
}

// Defaults 返回带默认值的配置。
func Defaults() *Config {
	return &Config{
		Listen:    ":8080",
		BaseDir:   "6v下载",
		MaxPages:  8,
		TokenFile: "token.json",
		SiteBase:  "http://www.6v520.com",
		TmdbLang:  "zh-CN",
	}
}

// Load 从 path 读取配置；文件不存在时返回默认值。
func Load(path string) (*Config, error) {
	c := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.BaseDir == "" {
		c.BaseDir = "6v下载"
	}
	if c.MaxPages <= 0 {
		c.MaxPages = 8
	}
	if c.TokenFile == "" {
		c.TokenFile = "token.json"
	}
	if c.SiteBase == "" {
		c.SiteBase = "http://www.6v520.com"
	}
	if c.TmdbLang == "" {
		c.TmdbLang = "zh-CN"
	}
	return c, nil
}
