package github_releases

import (
	"time"

	"github.com/alist-org/alist/v3/pkg/utils"
)

type File struct {
	FileName string    `json:"name"`
	Size     int64     `json:"size"`
	CreateAt time.Time `json:"time"`
	UpdateAt time.Time `json:"chtime"`
	Url      string    `json:"url"`
	Type     string    `json:"type"`
	Path     string    `json:"path"`
}

func (f File) GetHash() utils.HashInfo {
	return utils.HashInfo{}
}

func (f File) GetPath() string {
	return f.Path
}

func (f File) GetSize() int64 {
	return f.Size
}

func (f File) GetName() string {
	return f.FileName
}

func (f File) ModTime() time.Time {
	return f.UpdateAt
}

func (f File) CreateTime() time.Time {
	return f.CreateAt
}

func (f File) IsDir() bool {
	return f.Type == "dir"
}

func (f File) GetID() string {
	return f.Url
}

func (f File) Thumb() string {
	return ""
}
