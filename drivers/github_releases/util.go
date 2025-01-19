package github_releases

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/drivers/base"
	jsoniter "github.com/json-iterator/go"
)

type Repo struct {
	Path     string
	RepoName string
}

// 解析仓库列表
func ParseRepos(text string) ([]Repo, error) {
	lines := strings.Split(text, "\n")
	var repos []Repo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format: %s", line)
		}
		repos = append(repos, Repo{
			Path:     fmt.Sprintf("/%s", strings.Trim(parts[0], "/")),
			RepoName: parts[1],
		})
	}
	return repos, nil
}

// 获取 Github Releases
func RequestGithubReleases(repo string, basePath string) ([]File, error) {
	req := base.RestyClient.R()
	res, err := req.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", strings.Trim(repo, "/")))
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("request failed: %s", res.Status())
	}
	assets := jsoniter.Get(res.Body(), "assets")
	var files []File

	for i := 0; i < assets.Size(); i++ {
		filename := assets.Get(i, "name").ToString()

		files = append(files, File{
			FileName: filename,
			Size:     assets.Get(i, "size").ToInt64(),
			Url:      assets.Get(i, "browser_download_url").ToString(),
			Type:     assets.Get(i, "content_type").ToString(),
			Path:     fmt.Sprintf("%s/%s", basePath, filename),

			CreateAt: func() time.Time {
				t, _ := time.Parse(time.RFC3339, assets.Get(i, "created_at").ToString())
				return t
			}(),
			UpdateAt: func() time.Time {
				t, _ := time.Parse(time.RFC3339, assets.Get(i, "updated_at").ToString())
				return t
			}(),
		})
	}
	return files, nil
}

// 获取 README、LICENSE 等文件
func RequestGithubOtherFile(repo string, basePath string) ([]File, error) {
	req := base.RestyClient.R()
	res, err := req.Get(fmt.Sprintf("https://api.github.com/repos/%s/contents/", strings.Trim(repo, "/")))
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("request failed: %s", res.Status())
	}
	body := jsoniter.Get(res.Body())
	var files []File
	for i := 0; i < body.Size(); i++ {
		filename := body.Get(i, "name").ToString()

		re := regexp.MustCompile(`(?i)^(.*\.md|LICENSE)$`)

		if !re.MatchString(filename) {
			continue
		}

		files = append(files, File{
			FileName: filename,
			Size:     body.Get(i, "size").ToInt64(),
			CreateAt: time.Time{},
			UpdateAt: time.Now(),
			Url:      body.Get(i, "download_url").ToString(),
			Type:     body.Get(i, "type").ToString(),
			Path:     fmt.Sprintf("%s/%s", basePath, filename),
		})
	}
	return files, nil
}

// 获取下一级目录
func GetNextDir(wholePath string, basePath string) string {
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	if !strings.HasPrefix(wholePath, basePath) {
		return ""
	}
	remainingPath := strings.TrimLeft(strings.TrimPrefix(wholePath, basePath), "/")
	if remainingPath != "" {
		parts := strings.Split(remainingPath, "/")
		return parts[0]
	}
	return ""
}
