package sevenzip

import (
	"github.com/alist-org/alist/v3/internal/archive/tool"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"io"
	"os"
	stdpath "path"
	"strings"
)

type SevenZip struct{}

func (SevenZip) AcceptedExtensions() []string {
	return []string{".7z"}
}

func (SevenZip) AcceptedMultipartExtensions() []string {
	return []string{".7z.%.3d"}
}

func (SevenZip) GetMeta(ss []*stream.SeekableStream, args model.ArchiveArgs) (model.ArchiveMeta, error) {
	reader, err := getReader(ss, args.Password)
	if err != nil {
		return nil, err
	}
	// Build folder tree, same as zip
	dirMap := make(map[string]*model.ObjectTree)
	dirMap["."] = &model.ObjectTree{}
	for _, file := range reader.File {
		var dir string
		var dirObj *model.ObjectTree
		isNewFolder := false
		if !file.FileInfo().IsDir() {
			dir = stdpath.Dir(file.Name)
			dirObj = dirMap[dir]
			if dirObj == nil {
				isNewFolder = true
				dirObj = &model.ObjectTree{}
				dirObj.IsFolder = true
				dirObj.Name = stdpath.Base(dir)
				dirObj.Modified = file.Modified
				dirMap[dir] = dirObj
			}
			dirObj.Children = append(
				dirObj.Children, &model.ObjectTree{
					Object: *toModelObj(file.FileInfo()),
				},
			)
		} else {
			dir = strings.TrimSuffix(file.Name, "/")
			dirObj = dirMap[dir]
			if dirObj == nil {
				isNewFolder = true
				dirObj = &model.ObjectTree{}
				dirMap[dir] = dirObj
			}
			dirObj.IsFolder = true
			dirObj.Name = stdpath.Base(dir)
			dirObj.Modified = file.Modified
			dirObj.Children = make([]model.ObjTree, 0)
		}
		if isNewFolder {
			dir = stdpath.Dir(dir)
			pDirObj := dirMap[dir]
			if pDirObj != nil {
				pDirObj.Children = append(pDirObj.Children, dirObj)
				continue
			}
			for {
				pDirObj = &model.ObjectTree{}
				pDirObj.IsFolder = true
				pDirObj.Name = stdpath.Base(dir)
				pDirObj.Modified = file.Modified
				dirMap[dir] = pDirObj
				pDirObj.Children = append(pDirObj.Children, dirObj)
				dir = stdpath.Dir(dir)
				if dirMap[dir] != nil {
					break
				}
				dirObj = pDirObj
			}
		}
	}
	return &model.ArchiveMetaInfo{
		Comment:   "",
		Encrypted: args.Password != "",
		Tree:      dirMap["."].GetChildren(),
	}, nil
}

func (SevenZip) List(ss []*stream.SeekableStream, args model.ArchiveInnerArgs) ([]model.Obj, error) {
	return nil, errs.NotSupport
}

func (SevenZip) Extract(ss []*stream.SeekableStream, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	reader, err := getReader(ss, args.Password)
	if err != nil {
		return nil, 0, err
	}
	innerPath := strings.TrimPrefix(args.InnerPath, "/")
	for _, file := range reader.File {
		if file.Name == innerPath {
			r, e := file.Open()
			if e != nil {
				return nil, 0, e
			}
			return r, file.FileInfo().Size(), nil
		}
	}
	return nil, 0, errs.ObjectNotFound
}

func (SevenZip) Decompress(ss []*stream.SeekableStream, outputPath string, args model.ArchiveInnerArgs, up model.UpdateProgress) error {
	reader, err := getReader(ss, args.Password)
	if err != nil {
		return err
	}
	if args.InnerPath == "/" {
		for i, file := range reader.File {
			err = decompress(file, file.Name, outputPath)
			if err != nil {
				return err
			}
			up(float64(i+1) * 100.0 / float64(len(reader.File)))
		}
	} else {
		innerPath := strings.TrimPrefix(args.InnerPath, "/")
		innerBase := stdpath.Base(innerPath)
		createdBaseDir := false
		for _, file := range reader.File {
			if file.Name == innerPath {
				err = _decompress(file, outputPath, up)
				if err != nil {
					return err
				}
				break
			} else if strings.HasPrefix(file.Name, innerPath+"/") {
				targetPath := stdpath.Join(outputPath, innerBase)
				if !createdBaseDir {
					err = os.Mkdir(targetPath, 0700)
					if err != nil {
						return err
					}
					createdBaseDir = true
				}
				restPath := strings.TrimPrefix(file.Name, innerPath+"/")
				err = decompress(file, restPath, targetPath)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var _ tool.Tool = (*SevenZip)(nil)

func init() {
	tool.RegisterTool(SevenZip{})
}
