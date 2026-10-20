package cfg

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 是应用配置，对应 config.json。
type Config struct {
	Listen         string `json:"listen"`          // HTTP 监听地址，默认 :8080
	BaseDir        string `json:"base_dir"`        // 2dland 网盘内根目录名，默认 6v下载
	OrganizedDir   string `json:"organized_dir"`   // 下载完成后移动到 base_dir 下的目录名，默认 已下载
	MaxPages       int    `json:"max_pages"`       // 每个分类最大翻页数，默认 8
	ClientID       string `json:"client_id"`       // 2dland 开放平台 client_id
	ClientSecret   string `json:"client_secret"`   // 2dland 开放平台 client_secret
	AccessPassword string `json:"access_password"` // 访问 UI 的密码，留空表示未启用（首次进入走设置向导）
	TokenFile      string `json:"token_file"`      // token 持久化文件，默认 token.json
	SiteBase       string `json:"site_base"`       // 6v520 站点根，默认 https://www.6v520.com

	TmdbAPIKey string `json:"tmdb_api_key"`  // TMDB API Key，留空则不规范化
	TmdbProxy  string `json:"tmdb_proxy"`    // 访问 TMDB 的代理，如 http://127.0.0.1:7890
	TmdbLang   string `json:"tmdb_language"` // TMDB 语言，默认 zh-CN

	// 订阅追更
	SubIntervalMinutes int    `json:"sub_interval_minutes"` // 兼容旧全局检查间隔
	SubPushSafetyLimit int    `json:"sub_push_safety_limit"` // 单次自动推送集数安全上限，默认20
	OpenlistURL        string `json:"openlist_url"`         // OpenList/AList 地址，如 http://nas.zx.al:5244
	OpenlistUser       string `json:"openlist_user"`        // OpenList 账号
	OpenlistPass       string `json:"openlist_pass"`        // OpenList 密码
	OpenlistBase       string `json:"openlist_base"`        // 媒体库根路径（模糊搜索剧目录的起点），默认 /

	// 飞牛影视（fnOS）绑定
	FnosURL     string `json:"fnos_url"`      // 飞牛影视地址，如 http://nas.example.com:5666
	FnosUser    string `json:"fnos_user"`     // 飞牛影视账号
	FnosPass    string `json:"fnos_pass"`     // 飞牛影视密码
	FnosSecret  string `json:"fnos_secret"`   // 飞牛影视 API Secret（authx）
	FnosAPIKey  string `json:"fnos_api_key"`  // 飞牛影视 API Key（authx）
}

// Defaults 返回带默认值的配置。
func Defaults() *Config {
	return &Config{
		Listen:       ":8080",
		BaseDir:      "6v下载",
		OrganizedDir: "已下载",
		MaxPages:     8,
		TokenFile:    "token.json",
		SiteBase:     "https://www.6v520.com",
		TmdbLang:     "zh-CN",
		SubIntervalMinutes: 120,
		SubPushSafetyLimit: 20,
		OpenlistBase:       "/",
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
	if c.OrganizedDir == "" {
		c.OrganizedDir = "已下载"
	}
	if c.MaxPages <= 0 {
		c.MaxPages = 8
	}
	if c.TokenFile == "" {
		c.TokenFile = "token.json"
	}
	if c.SiteBase == "" {
		c.SiteBase = "https://www.6v520.com"
	}
	if c.TmdbLang == "" {
		c.TmdbLang = "zh-CN"
	}
	return c, nil
}

// Save 将配置以缩进 JSON 原子写入 path（权限 0600，含敏感信息）。
func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
