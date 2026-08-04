package drive

import (
	"context"
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
	base, err := ensureDir(ctx, s.userfile, "/", s.baseDir)
	if err != nil {
		return "", err
	}
	cat, err := ensureDir(ctx, s.userfile, base, categoryName(category))
	if err != nil {
		return "", err
	}
	titlePath, err := ensureDir(ctx, s.userfile, cat, titleName)
	if err != nil {
		return "", err
	}
	if isTVCategory(category) && seasonName != "" {
		return ensureDir(ctx, s.userfile, titlePath, seasonName)
	}
	return titlePath, nil
}

// ensureDir 在 parentPath 下确保名为 name 的目录存在，返回其完整路径。
func ensureDir(ctx context.Context, uf *userfile.UserFileService, parentPath, name string) (string, error) {
	if existing, _ := findDir(ctx, uf, parentPath, name); existing != nil {
		if existing.Path != "" {
			return existing.Path, nil
		}
		return joinPath(parentPath, name), nil
	}
	created, err := uf.Create(ctx, &userfile.File{
		Name:   name,
		Dir:    true,
		Parent: parentPath,
	})
	if err != nil {
		return "", err
	}
	if created.Path != "" {
		return created.Path, nil
	}
	return joinPath(parentPath, name), nil
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
