package main

import (
	"embed"
	"log"
	"net/http"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/server"
)

//go:embed all:web
var webFS embed.FS

func main() {
	c, err := cfg.Load("config.json")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		log.Printf("警告: config.json 未填写 client_id/client_secret，请先在 2dland 开放平台申请并填写后再使用离线下载功能。")
	}
	srv := server.New(c, webFS)
	log.Printf("6v520 → 2dland 助手已启动，监听 %s", c.Listen)
	if err := http.ListenAndServe(c.Listen, srv.Routes()); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
