package sevenzip

import (
	"errors"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/bodgit/sevenzip"
	"io"
	"os"
	stdpath "path"
)

func toModelObj(file os.FileInfo) *model.Object {
	return &model.Object{
		Name:     file.Name(),
		Size:     file.Size(),
		Modified: file.ModTime(),
		IsFolder: file.IsDir(),
	}
}

func getReader(ss []*stream.SeekableStream, password string) (*sevenzip.Reader, error) {
	readerAt, err := stream.NewMultiReaderAt(ss)
	if err != nil {
		return nil, err
	}
	sr, err := sevenzip.NewReaderWithPassword(readerAt, readerAt.Size(), password)
	if err != nil {
		return nil, filterPassword(err)
	}
	return sr, nil
}

func filterPassword(err error) error {
	if err != nil {
		var e *sevenzip.ReadError
		if errors.As(err, &e) && e.Encrypted {
			return errs.WrongArchivePassword
		}
	}
	return err
}

func decompress(file *sevenzip.File, filePath, outputPath string) error {
	targetPath := outputPath
	dir, base := stdpath.Split(filePath)
	if dir != "" {
		targetPath = stdpath.Join(targetPath, dir)
		err := os.MkdirAll(targetPath, 0700)
		if err != nil {
			return err
		}
	}
	if base != "" {
		err := _decompress(file, targetPath, func(_ float64) {})
		if err != nil {
			return err
		}
	}
	return nil
}

func _decompress(file *sevenzip.File, targetPath string, up model.UpdateProgress) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	f, err := os.OpenFile(stdpath.Join(targetPath, file.FileInfo().Name()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, &stream.ReaderUpdatingProgress{
		Reader: &stream.SimpleReaderWithSize{
			Reader: rc,
			Size:   file.FileInfo().Size(),
		},
		UpdateProgress: up,
	})
	if err != nil {
		return err
	}
	return nil
}
