package smb

import (
	"context"
	"io"
	"net/http"
	"os"
	stdpath "path"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/pkg/errors"
)

type writingFile struct {
	path string
	f    *os.File
}

func newUpload(ctx context.Context, path string) (*writingFile, error) {
	err := uploadAuth(ctx, path)
	if err != nil {
		return nil, err
	}
	tmpFile, err := os.CreateTemp(conf.Conf.TempDir, "file-*")
	if err != nil {
		return nil, err
	}
	return &writingFile{f: tmpFile, path: path}, nil
}

func uploadAuth(ctx context.Context, path string) error {
	user := ctx.Value("user").(*model.User)
	meta, err := op.GetNearestMeta(stdpath.Dir(path))
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			return err
		}
	}
	if !(common.CanAccess(user, meta, path, "") &&
		((user.CanSMBManage() && user.CanWrite()) || common.CanWrite(meta, stdpath.Dir(path)))) {
		return errs.PermissionDenied
	}
	return nil
}

func (f *writingFile) close(ctx context.Context) error {
	dir, name := stdpath.Split(f.path)
	size, err := f.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := f.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	arr := make([]byte, 512)
	if _, err := f.f.Read(arr); err != nil {
		return err
	}
	contentType := http.DetectContentType(arr)
	if _, err := f.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_ = fs.Remove(ctx, f.path)
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     size,
			Modified: time.Now(),
		},
		Mimetype:     contentType,
		WebPutAsTask: true,
	}
	s.SetTmpFile(f.f)
	_, err = fs.PutAsTask(ctx, dir, s)
	return err
}
