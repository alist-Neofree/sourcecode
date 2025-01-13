package tool

import (
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"io"
)

var (
	Tools = make(map[string]Tool)
)

func RegisterTool(tool Tool) {
	for _, ext := range tool.AcceptedExtensions() {
		Tools[ext] = tool
	}
}

func GetArchiveTool(ext string) (Tool, error) {
	t, ok := Tools[ext]
	if !ok {
		return nil, errs.UnknownArchiveFormat
	}
	return t, nil
}

type SequentialFile struct {
	Reader io.ReadCloser
}

func (s *SequentialFile) Read(p []byte) (n int, err error) {
	return s.Reader.Read(p)
}

func (s *SequentialFile) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, errs.NotSupport
}

func (s *SequentialFile) Seek(offset int64, whence int) (int64, error) {
	return 0, errs.NotSupport
}

func (s *SequentialFile) Close() error {
	return s.Reader.Close()
}

type EmptyMeta struct{}

func (e *EmptyMeta) GetComment() string {
	return ""
}

func (e *EmptyMeta) IsEncrypted() bool {
	return false
}

func (e *EmptyMeta) GetTree() []model.ObjTree {
	return nil
}
