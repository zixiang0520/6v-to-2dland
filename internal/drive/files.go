package drive

import (
	"context"
	"log"

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

// Mkdir 在 parentPath 下创建名为 name 的文件夹，返回创建后的文件信息。
func (c *Client) Mkdir(ctx context.Context, parentPath, name string) (*userfile.File, error) {
	s := c.snap()
	f, err := s.userfile.Create(ctx, &userfile.File{
		Name:   name,
		Dir:    true,
		Parent: parentPath,
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
func (c *Client) Move(ctx context.Context, identities []string, destPath string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	s := c.snap()
	srcs := make([]*userfile.File, 0, len(identities))
	for _, id := range identities {
		srcs = append(srcs, &userfile.File{Identity: id})
	}
	_, err := s.userfile.Move(ctx, &userfile.BatchOperationRequest{
		Source: srcs,
		Dest:   &userfile.File{Path: destPath},
	})
	return err
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
