package drive

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/model"
	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/userfile"
)

// ListFiles 列出 parentPath 下的全部文件/目录（游标分页循环拉取，每页 50）。
func (c *Client) ListFiles(ctx context.Context, parentPath string) ([]*userfile.File, error) {
	s := c.snap()
	const pageSize = 50
	var all []*userfile.File
	var token string
	for page := 0; ; page++ {
		resp, err := s.userfile.List(ctx, &userfile.FileListRequest{
			Parent:   &userfile.File{Path: parentPath},
			ListInfo: &model.ScanListRequest{Limit: pageSize, Token: token},
		})
		if err != nil {
			if page == 0 {
				return nil, err
			}
			log.Printf("ListFiles: page %d error: %v (returning %d files so far)", page, err, len(all))
			break
		}
		all = append(all, resp.Files...)
		if resp.ListInfo == nil || resp.ListInfo.Token == "" || len(resp.Files) == 0 {
			break
		}
		token = resp.ListInfo.Token
		if page > 100 {
			break
		}
	}
	return all, nil
}

// FillDirStats 给列表里的目录补齐真实条目数。
// 2dland 的 list 接口对目录的 files/direcotries 字段经常返回 0（不统计子项），
// 界面就会显示「0 文件夹 · 0 文件」，用户以为目录是空的。
// 只补当前层、只补统计为 0 的目录；目录数量过多时整体跳过，避免一次打出几十个请求触发限流。
func (c *Client) FillDirStats(ctx context.Context, files []*userfile.File) {
	const (
		maxFill  = 40
		parallel = 3
	)
	var todo []*userfile.File
	for _, f := range files {
		if f.Dir && f.Files == 0 && f.Direcotries == 0 {
			todo = append(todo, f)
		}
	}
	if len(todo) == 0 || len(todo) > maxFill {
		return
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, f := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func(f *userfile.File) {
			defer wg.Done()
			defer func() { <-sem }()
			kids, err := c.ListFiles(ctx, f.Path)
			if err != nil {
				log.Printf("FillDirStats: list %q failed: %v", f.Path, err)
				return
			}
			var nf, nd int64
			for _, k := range kids {
				if k.Dir {
					nd++
				} else {
					nf++
				}
			}
			f.Files, f.Direcotries = nf, nd
		}(f)
	}
	wg.Wait()
}

// removeDirIfEmpty 目录里既没文件也没子目录时把它删掉（进回收站，可恢复）。
// 只用于整理后残留的空项目目录，不递归、不动有内容的目录。
func (c *Client) removeDirIfEmpty(ctx context.Context, dirPath string) {
	dirPath = strings.TrimRight(dirPath, "/")
	if dirPath == "" || dirPath == "/" {
		return
	}
	kids, err := c.ListFiles(ctx, dirPath)
	if err != nil || len(kids) > 0 {
		return
	}
	s := c.snap()
	d, err := s.userfile.Get(ctx, &userfile.File{Path: dirPath})
	if err != nil || d == nil || d.Identity == "" {
		return
	}
	if err := c.DeleteFiles(ctx, []string{d.Identity}); err != nil {
		log.Printf("removeDirIfEmpty: delete %q failed: %v", dirPath, err)
		return
	}
	log.Printf("removeDirIfEmpty: removed empty dir %q", dirPath)
}

// Mkdir 在 parentPath 下创建名为 name 的文件夹，返回创建后的文件信息。
// 注意：2dland Create 的 Parent 字段需要 identity（不是 path），否则创建到根目录。
func (c *Client) Mkdir(ctx context.Context, parentPath, name string) (*userfile.File, error) {
	s := c.snap()
	parent, err := s.userfile.Get(ctx, &userfile.File{Path: parentPath})
	if err != nil || parent == nil || parent.Identity == "" {
		return nil, fmt.Errorf("父目录 %q 不存在: %v", parentPath, err)
	}
	f, err := s.userfile.Create(ctx, &userfile.File{
		Name:   name,
		Dir:    true,
		Parent: parent.Identity,
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Rename 重命名文件/目录（identity + 新名字）。
func (c *Client) Rename(ctx context.Context, identity, newName string) error {
	s := c.snap()
	_, err := s.userfile.Rename(ctx, &userfile.File{Identity: identity, Name: newName})
	return err
}

// Move 把一组文件/目录（按 identity）移动到 destPath 目录下。
// 注意：2dland Move 的 Dest 用 Path 会丢失文件（移到无效位置），必须用 Identity。
// 这里先用 Get 解析 destPath → identity，再用 Dest.Identity 移动。
func (c *Client) Move(ctx context.Context, identities []string, destPath string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	s := c.snap()
	dest, err := s.userfile.Get(ctx, &userfile.File{Path: destPath})
	if err != nil || dest == nil || dest.Identity == "" {
		return fmt.Errorf("移动目标目录 %q 不存在: %v", destPath, err)
	}
	srcs := make([]*userfile.File, 0, len(identities))
	for _, id := range identities {
		srcs = append(srcs, &userfile.File{Identity: id})
	}
	_, err = s.userfile.Move(ctx, &userfile.BatchOperationRequest{
		Source: srcs,
		Dest:   &userfile.File{Identity: dest.Identity},
	})
	return err
}

// ListRecentFiles 列出最近更新的文件（全局，用于查找丢失文件等）。
func (c *Client) ListRecentFiles(ctx context.Context) ([]*userfile.File, error) {
	s := c.snap()
	var all []*userfile.File
	var token string
	for page := 0; page < 20; page++ {
		resp, err := s.userfile.ListRecentUpdatedFiles(ctx, &userfile.ListRecentUpdatedFilesRequest{
			ListInfo: &model.ScanListRequest{Limit: 50, Token: token},
		})
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		all = append(all, resp.Files...)
		if resp.ListInfo == nil || resp.ListInfo.Token == "" || len(resp.Files) == 0 {
			break
		}
		token = resp.ListInfo.Token
	}
	return all, nil
}

// DeleteFiles 把一组文件/目录移到回收站（安全删除，可恢复）。
func (c *Client) DeleteFiles(ctx context.Context, identities []string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	s := c.snap()
	srcs := make([]*userfile.File, 0, len(identities))
	for _, id := range identities {
		srcs = append(srcs, &userfile.File{Identity: id})
	}
	_, err := s.userfile.Trash(ctx, &userfile.BatchOperationRequest{Source: srcs})
	return err
}

// ListTrash 列出回收站全部文件/目录（游标分页拉取）。
func (c *Client) ListTrash(ctx context.Context) ([]*userfile.File, error) {
	s := c.snap()
	const pageSize = 50
	var all []*userfile.File
	var token string
	for page := 0; ; page++ {
		resp, err := s.userfile.ListTrash(ctx, &userfile.FileListRequest{
			ListInfo: &model.ScanListRequest{Limit: pageSize, Token: token},
		})
		if err != nil {
			if page == 0 {
				return nil, err
			}
			log.Printf("ListTrash: page %d error: %v (returning %d items so far)", page, err, len(all))
			break
		}
		all = append(all, resp.Files...)
		if resp.ListInfo == nil || resp.ListInfo.Token == "" || len(resp.Files) == 0 {
			break
		}
		token = resp.ListInfo.Token
		if page > 100 {
			break
		}
	}
	return all, nil
}

// Recover 从回收站恢复一组文件/目录（按 identity）。
func (c *Client) Recover(ctx context.Context, identities []string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	s := c.snap()
	srcs := make([]*userfile.File, 0, len(identities))
	for _, id := range identities {
		srcs = append(srcs, &userfile.File{Identity: id})
	}
	_, err := s.userfile.Recover(ctx, &userfile.BatchOperationRequest{Source: srcs})
	return err
}
