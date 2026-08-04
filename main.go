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
	const cfgPath = "config.json"
	c, err := cfg.Load(cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if c.AccessPassword == "" {
		log.Printf("提示: 首次启动，请在浏览器打开 http://localhost%s 完成初始化向导", c.Listen)
	}
	srv := server.New(c, cfgPath, webFS)
	log.Printf("6v520 → 2dland 助手已启动，监听 %s", c.Listen)
	if err := http.ListenAndServe(c.Listen, srv.Routes()); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
