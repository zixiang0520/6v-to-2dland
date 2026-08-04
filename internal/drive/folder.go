package drive

import (
	"context"
	"log"
	"strings"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

// categoryNames 把 6v520 分类目录名映射为中文目录名。
var categoryNames = map[string]string{
	"dy": "电影", "gydy": "国语电影", "gq": "经典高清",
	"zydy": "动漫", "jddy": "动画电影", "3D": "3D电影",
	"dlz": "国剧", "rj": "日韩剧", "mj": "欧美剧",
	"zy": "综艺", "shoujidianyingmp4": "手机电影",
}

// tvCategories 是剧集类分类（会建立第三级「季」目录）。
var tvCategories = map[string]bool{
	"dlz": true, "rj": true, "mj": true, "zy": true,
}

func categoryName(cat string) string {
	if n, ok := categoryNames[cat]; ok {
		return n
	}
	if cat == "" {
		return "未分类"
	}
	return cat
}

func isTVCategory(cat string) bool { return tvCategories[cat] }

// sanitize 清理文件夹名中的非法字符。
func sanitize(name string) string {
	r := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	s := strings.TrimSpace(r.Replace(name))
	if s == "" {
		s = "未命名"
	}
	return s
}

// ensureFolderByCategory 按 /<baseDir>/<分类>/<标题>[/<季>] 建目录。
// 剧集类建立第三级 seasonName 目录；电影类忽略 seasonName，止于标题目录。
func ensureFolderByCategory(ctx context.Context, s snapshot, category, titleName, seasonName string) (string, error) {
	log.Printf("ensureFolderByCategory: category=%q titleName=%q seasonName=%q baseDir=%q", category, titleName, seasonName, s.baseDir)
	base, err := ensureDir(ctx, s.userfile, "/", s.baseDir)
	if err != nil {
		log.Printf("ensureFolderByCategory: ensure baseDir failed: %v", err)
		return "", err
	}
	cat, err := ensureDir(ctx, s.userfile, base, categoryName(category))
	if err != nil {
		log.Printf("ensureFolderByCategory: ensure category failed: %v", err)
		return "", err
	}
	titlePath, err := ensureDir(ctx, s.userfile, cat, titleName)
	if err != nil {
		log.Printf("ensureFolderByCategory: ensure title failed: %v", err)
		return "", err
	}
	if isTVCategory(category) && seasonName != "" {
		sp, err := ensureDir(ctx, s.userfile, titlePath, seasonName)
		if err != nil {
			log.Printf("ensureFolderByCategory: ensure season failed: %v", err)
			return "", err
		}
		log.Printf("ensureFolderByCategory: done savePath=%q", sp)
		return sp, nil
	}
	log.Printf("ensureFolderByCategory: done savePath=%q", titlePath)
	return titlePath, nil
}

// ensureDir 在 parentPath 下确保名为 name 的目录存在，返回其完整路径。
// 防御：2dland API 返回的 File.Path 偶发只是目录名（非 / 开头的相对路径），
// 直接用作下一步的 parent 或 offline_task/add 的 save_path 会导致目录错位
// （表现为只建出最后一级「第N季」）。这里统一校验：返回值必须以 parentPath
// 为前缀，否则用 joinPath(parentPath, name) 兜底拼出完整路径。
func ensureDir(ctx context.Context, uf *userfile.UserFileService, parentPath, name string) (string, error) {
	if existing, _ := findDir(ctx, uf, parentPath, name); existing != nil {
		p := normalizePath(existing.Path, parentPath, name)
		log.Printf("ensureDir: hit existing parent=%q name=%q apiPath=%q usePath=%q", parentPath, name, existing.Path, p)
		return p, nil
	}
	created, err := uf.Create(ctx, &userfile.File{
		Name:   name,
		Dir:    true,
		Parent: parentPath,
	})
	if err != nil {
		return "", err
	}
	p := normalizePath(created.Path, parentPath, name)
	log.Printf("ensureDir: created parent=%q name=%q apiPath=%q usePath=%q", parentPath, name, created.Path, p)
	return p, nil
}

// normalizePath 校验 apiPath 是否为相对于根的完整路径；不是则用 wantPath 兜底。
// 判定标准：以 "/" 开头，且是 parentPath 的自身或子路径。
func normalizePath(apiPath, parentPath, name string) string {
	wantPath := joinPath(parentPath, name)
	if apiPath == "" {
		return wantPath
	}
	if !strings.HasPrefix(apiPath, "/") {
		return wantPath
	}
	// parentPath 是 "/" 时，任何 /name 都合法
	if parentPath == "/" {
		return apiPath
	}
	if apiPath == parentPath || strings.HasPrefix(apiPath, strings.TrimRight(parentPath, "/")+"/") {
		return apiPath
	}
	return wantPath
}

// findDir 在 parentPath 下查找名为 name 的子目录。
func findDir(ctx context.Context, uf *userfile.UserFileService, parentPath, name string) (*userfile.File, error) {
	resp, err := uf.List(ctx, &userfile.FileListRequest{
		Parent: &userfile.File{Path: parentPath},
	})
	if err != nil {
		return nil, err
	}
	for _, f := range resp.Files {
		if f.Dir && f.Name == name {
			return f, nil
		}
	}
	return nil, nil
}

func joinPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return strings.TrimRight(parent, "/") + "/" + name
}
