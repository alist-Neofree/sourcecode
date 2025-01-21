package github_releases

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
)

var (
	cache   = make(map[string]*resty.Response)
	created = make(map[string]time.Time)
	mu      sync.Mutex
)

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

// 发送 GET 请求
func GetRequest(url string, CacheExpiration int) (*resty.Response, error) {
	mu.Lock()
	if res, ok := cache[url]; ok && time.Now().Before(created[url].Add(time.Duration(CacheExpiration)*time.Minute)) {
		mu.Unlock()
		return res, nil
	}
	mu.Unlock()

	res, err := base.RestyClient.R().Get(url)
	if err != nil || res.StatusCode() != 200 {
		return nil, fmt.Errorf("request fail: %v", err)
	}

	mu.Lock()
	cache[url] = res
	created[url] = time.Now()
	mu.Unlock()

	return res, nil
}

// 获取 README、LICENSE 等文件
func GetGithubOtherFile(repo string, basePath string, CacheExpiration int) (*[]File, error) {
	res, _ := GetRequest(
		fmt.Sprintf("https://api.github.com/repos/%s/contents/", strings.Trim(repo, "/")),
		CacheExpiration,
	)
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
	return &files, nil
}

// 获取 GitHub Release 详细信息
func GetRepoReleaseInfo(repo string, basePath string, CacheExpiration int) (*GithubReleasesData, error) {
	res, _ := GetRequest(
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", strings.Trim(repo, "/")),
		CacheExpiration,
	)
	body := res.Body()
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

	return &GithubReleasesData{
		Files: files,
		Url:   jsoniter.Get(body, "html_url").ToString(),

		Size: func() int64 {
			size := int64(0)
			for _, file := range files {
				size += file.Size
			}
			return size
		}(),
		UpdateAt: func() time.Time {
			t, _ := time.Parse(time.RFC3339, jsoniter.Get(body, "published_at").ToString())
			return t
		}(),
		CreateAt: func() time.Time {
			t, _ := time.Parse(time.RFC3339, jsoniter.Get(body, "created_at").ToString())
			return t
		}(),
	}, nil
}
