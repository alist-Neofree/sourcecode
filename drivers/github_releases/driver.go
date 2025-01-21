package github_releases

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"strings"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
)

type GithubReleases struct {
	model.Storage
	Addition

	repoList []Repo
}

func (d *GithubReleases) Config() driver.Config {
	return config
}

func (d *GithubReleases) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *GithubReleases) Init(ctx context.Context) error {
	repos, err := ParseRepos(d.Addition.RepoStructure)
	if err != nil {
		return err
	}
	d.repoList = repos
	return nil
}

func (d *GithubReleases) Drop(ctx context.Context) error {
	return nil
}

func (d *GithubReleases) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files := make([]File, 0)
	path := fmt.Sprintf("/%s", strings.Trim(dir.GetPath(), "/"))

	for _, repo := range d.repoList {
		if repo.Path == path { // 与仓库路径相同
			resp, err := GetRepoReleaseInfo(repo.RepoName, path, d.Storage.CacheExpiration)
			if err != nil {
				return nil, err
			}
			files = append(files, resp.Files...)

			if d.Addition.ShowReadme {
				resp, err := GetGithubOtherFile(repo.RepoName, path, d.Storage.CacheExpiration)
				if err != nil {
					return nil, err
				}
				files = append(files, *resp...)
			}

		} else if strings.HasPrefix(repo.Path, path) { // 仓库路径是目录的子目录
			nextDir := GetNextDir(repo.Path, path)
			if nextDir == "" {
				continue
			}
			repo, _ := GetRepoReleaseInfo(repo.RepoName, path, d.Storage.CacheExpiration)

			hasSameDir := false
			for index, file := range files {
				if file.FileName == nextDir {
					hasSameDir = true
					files[index].Size += repo.Size
					files[index].UpdateAt = func(a time.Time, b time.Time) time.Time {
						if a.After(b) {
							return a
						}
						return b
					}(files[index].UpdateAt, repo.UpdateAt)
					break
				}
			}

			if !hasSameDir {
				files = append(files, File{
					FileName: nextDir,
					Size:     repo.Size,
					CreateAt: repo.CreateAt,
					UpdateAt: repo.UpdateAt,
					Url:      repo.Url,
					Type:     "dir",
					Path:     fmt.Sprintf("%s/%s", path, nextDir),
				})
			}
		}
	}

	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return src, nil
	})
}

func (d *GithubReleases) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	link := model.Link{
		URL:    file.GetID(),
		Header: http.Header{},
	}
	return &link, nil
}

func (d *GithubReleases) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *GithubReleases) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *GithubReleases) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *GithubReleases) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *GithubReleases) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotImplement
}

func (d *GithubReleases) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	return nil, errs.NotImplement
}

var _ driver.Driver = (*GithubReleases)(nil)
