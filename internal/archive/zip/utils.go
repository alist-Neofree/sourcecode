package zip

import (
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/yeka/zip"
	"io"
	"os"
	stdpath "path"
)

type zipMeta struct {
	Comment   string
	Encrypted bool
}

func (m *zipMeta) GetComment() string {
	return m.Comment
}

func (m *zipMeta) IsEncrypted() bool {
	return m.Encrypted
}

func (m *zipMeta) GetTree() []model.ObjTree {
	return nil
}

func toModelObj(file os.FileInfo) *model.Object {
	return &model.Object{
		Name:     file.Name(),
		Size:     file.Size(),
		Modified: file.ModTime(),
		IsFolder: file.IsDir(),
	}
}

func decompress(file *zip.File, filePath, outputPath, password string) error {
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
		err := _decompress(file, targetPath, password, func(_ float64) {})
		if err != nil {
			return err
		}
	}
	return nil
}

func _decompress(file *zip.File, targetPath, password string, up model.UpdateProgress) error {
	if file.IsEncrypted() {
		file.SetPassword(password)
	}
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
