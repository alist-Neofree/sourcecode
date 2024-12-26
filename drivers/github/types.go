package github

import (
	"github.com/alist-org/alist/v3/internal/model"
	"time"
)

type Links struct {
	Git  string `json:"git"`
	Html string `json:"html"`
	Self string `json:"self"`
}

type Object struct {
	Type            string   `json:"type"`
	Encoding        string   `json:"encoding" required:"false"`
	Size            int64    `json:"size"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	Content         string   `json:"Content" required:"false"`
	Sha             string   `json:"sha"`
	URL             string   `json:"url"`
	GitURL          string   `json:"git_url"`
	HtmlURL         string   `json:"html_url"`
	DownloadURL     string   `json:"download_url"`
	Entries         []Object `json:"entries" required:"false"`
	Links           Links    `json:"_links"`
	SubmoduleGitURL string   `json:"submodule_git_url" required:"false"`
	Target          string   `json:"target" required:"false"`
}

func (o *Object) toModelObj() *model.Object {
	return &model.Object{
		Name:     o.Name,
		Size:     o.Size,
		Modified: time.Unix(0, 0),
		IsFolder: o.Type == "dir",
	}
}

type PutResp struct {
	Content Object      `json:"Content"`
	Commit  interface{} `json:"commit"`
}

type ErrResp struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url"`
	Status           string `json:"status"`
}
