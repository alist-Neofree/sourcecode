package ipfs

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	shell "github.com/ipfs/go-ipfs-api"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
)

type IPFS struct {
	model.Storage
	Addition
	sh      *shell.Shell
	gateURL *url.URL
}

func (d *IPFS) Config() driver.Config {
	return config
}

func (d *IPFS) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *IPFS) Init(ctx context.Context) error {
	d.sh = shell.NewShell(d.Endpoint)
	gateURL, err := url.Parse(d.Gateway)
	if err != nil {
		return err
	}
	d.gateURL = gateURL
	return nil
}

func (d *IPFS) Drop(ctx context.Context) error {
	return nil
}

func (d *IPFS) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	ipfsPath := dir.GetPath()
	switch d.Mode {
	case "ipfs":
		ipfsPath = filepath.Join("/ipfs", ipfsPath)
	case "ipns":
		ipfsPath = filepath.Join("/ipns", ipfsPath)
	case "mfs":
		fileStat, err := d.sh.FilesStat(ctx, ipfsPath)
		if err != nil {
			return nil, err
		}
		ipfsPath = filepath.Join("/ipfs", fileStat.Hash)
	default:
		return nil, fmt.Errorf("mode error")
	}
	dirs, err := d.sh.List(ipfsPath)
	if err != nil {
		return nil, err
	}

	objlist := []model.Obj{}
	for _, file := range dirs {
		objlist = append(objlist, &model.Object{ID: file.Hash, Name: file.Name, Size: int64(file.Size), IsFolder: file.Type == 1})
	}

	return objlist, nil
}

func (d *IPFS) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	gateurl := d.gateURL.JoinPath("/ipfs/", file.GetID())
	gateurl.RawQuery = "filename=" + url.QueryEscape(file.GetName())
	return &model.Link{URL: gateurl.String()}, nil
}

func (d *IPFS) Get(ctx context.Context, path string) (model.Obj, error) {
	file, err := d.sh.FilesStat(ctx, path)
	if err != nil {
		return nil, err
	}
	return &model.Object{ID: file.Hash, Name: filepath.Base(path), Path: path, Size: int64(file.Size), IsFolder: file.Type == "directory"}, nil
}

func (d *IPFS) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	if d.Mode != "mfs" {
		return nil, fmt.Errorf("only write in mfs mode")
	}
	path := parentDir.GetPath()
	err := d.sh.FilesMkdir(ctx, filepath.Join(path, dirName), shell.FilesMkdir.Parents(true))
	if err != nil {
		return nil, err
	}
	file, err := d.sh.FilesStat(ctx, filepath.Join(path, dirName))
	if err != nil {
		return nil, err
	}
	return &model.Object{ID: file.Hash, Name: dirName, Path: filepath.Join(path, dirName), Size: int64(file.Size), IsFolder: true}, nil
}

func (d *IPFS) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	if d.Mode != "mfs" {
		return nil, fmt.Errorf("only write in mfs mode")
	}
	d.sh.FilesRm(ctx, dstDir.GetPath(), true)
	return &model.Object{ID: srcObj.GetID(), Name: srcObj.GetName(), Path: dstDir.GetPath(), Size: int64(srcObj.GetSize()), IsFolder: srcObj.IsDir()},
		d.sh.FilesMv(ctx, srcObj.GetPath(), dstDir.GetPath())
}

func (d *IPFS) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	if d.Mode != "mfs" {
		return nil, fmt.Errorf("only write in mfs mode")
	}
	dstPath := filepath.Join(filepath.Dir(srcObj.GetPath()), newName)
	d.sh.FilesRm(ctx, dstPath, true)
	return &model.Object{ID: srcObj.GetID(), Name: newName, Path: dstPath, Size: int64(srcObj.GetSize()),
		IsFolder: srcObj.IsDir()}, d.sh.FilesMv(ctx, srcObj.GetPath(), dstPath)
}

func (d *IPFS) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	if d.Mode != "mfs" {
		return nil, fmt.Errorf("only write in mfs mode")
	}
	dstPath := filepath.Join(dstDir.GetPath(), filepath.Base(srcObj.GetPath()))
	d.sh.FilesRm(ctx, dstPath, true)
	return &model.Object{ID: srcObj.GetID(), Name: srcObj.GetName(), Path: dstPath, Size: int64(srcObj.GetSize()), IsFolder: srcObj.IsDir()},
		d.sh.FilesCp(ctx, filepath.Join("/ipfs/", srcObj.GetID()), dstPath, shell.FilesCp.Parents(true))
}

func (d *IPFS) Remove(ctx context.Context, obj model.Obj) error {
	if d.Mode != "mfs" {
		return fmt.Errorf("only write in mfs mode")
	}
	return d.sh.FilesRm(ctx, obj.GetPath(), true)
}

func (d *IPFS) Put(ctx context.Context, dstDir model.Obj, s model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	if d.Mode != "mfs" {
		return nil, fmt.Errorf("only write in mfs mode")
	}
	outHash, err := d.sh.Add(driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
		Reader:         s,
		UpdateProgress: up,
	}))
	if err != nil {
		return nil, err
	}
	if s.GetExist() != nil {
		d.sh.FilesRm(ctx, filepath.Join(dstDir.GetPath(), s.GetName()), true)
	}
	err = d.sh.FilesCp(ctx, filepath.Join("/ipfs/", outHash), filepath.Join(dstDir.GetPath(), s.GetName()), shell.FilesCp.Parents(true))
	gateurl := d.gateURL.JoinPath("/ipfs/", outHash)
	gateurl.RawQuery = "filename=" + url.QueryEscape(s.GetName())
	return &model.Object{ID: outHash, Name: s.GetName(), Path: filepath.Join(dstDir.GetPath(), s.GetName()), Size: int64(s.GetSize()), IsFolder: s.IsDir()}, err
}

//func (d *Template) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
//	return nil, errs.NotSupport
//}

var _ driver.Driver = (*IPFS)(nil)
