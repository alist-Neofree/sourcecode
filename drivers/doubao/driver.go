package doubao

import (
	"context"
	"time"

	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/go-resty/resty/v2"
)

type Doubao struct {
	model.Storage
	Addition
}

func (d *Doubao) Config() driver.Config {
	return config
}

func (d *Doubao) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Doubao) Init(ctx context.Context) error {
	// TODO login / refresh token
	//op.MustSaveDriverStorage(d)
	return nil
}

func (d *Doubao) Drop(ctx context.Context) error {
	return nil
}

func (d *Doubao) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	var files []model.Obj
	var r NodeInfoResp
	_, err := d.request("/samantha/aispace/node_info", "POST", func(req *resty.Request) {
		req.SetBody(base.Json{
			"node_id":        dir.GetID(),
			"need_full_path": false,
		})
	}, &r)
	if err != nil {
		return nil, err
	}

	for _, child := range r.Data.Children {
		isFolder := child.NodeType == 1
		id := child.Key
		if isFolder {
			id = child.ID
		}
		files = append(files, &model.Object{
			ID:       id,
			Path:     child.ParentID,
			Name:     child.Name,
			Size:     int64(child.Size),
			Modified: time.Unix(int64(child.UpdateTime), 0),
			Ctime:    time.Unix(int64(child.CreateTime), 0),
			IsFolder: isFolder,
		})
	}
	return files, nil
}

func (d *Doubao) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	var r GetFileUrlResp
	_, err := d.request("/alice/message/get_file_url", "POST", func(req *resty.Request) {
		req.SetBody(base.Json{
			"uris": []string{file.GetID()},
			"type": "file",
		})
	}, &r)
	if err != nil {
		return nil, err
	}

	return &model.Link{
		URL: r.Data.FileUrls[0].MainURL,
	}, nil
}

func (d *Doubao) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	// TODO create folder, optional
	return nil, errs.NotImplement
}

func (d *Doubao) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// TODO move obj, optional
	return nil, errs.NotImplement
}

func (d *Doubao) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	// TODO rename obj, optional
	return nil, errs.NotImplement
}

func (d *Doubao) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// TODO copy obj, optional
	return nil, errs.NotImplement
}

func (d *Doubao) Remove(ctx context.Context, obj model.Obj) error {
	// TODO remove obj, optional
	return errs.NotImplement
}

func (d *Doubao) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	// TODO upload file, optional
	return nil, errs.NotImplement
}

func (d *Doubao) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
	// TODO get archive file meta-info, return errs.NotImplement to use an internal archive tool, optional
	return nil, errs.NotImplement
}

func (d *Doubao) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
	// TODO list args.InnerPath in the archive obj, return errs.NotImplement to use an internal archive tool, optional
	return nil, errs.NotImplement
}

func (d *Doubao) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
	// TODO return link of file args.InnerPath in the archive obj, return errs.NotImplement to use an internal archive tool, optional
	return nil, errs.NotImplement
}

func (d *Doubao) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
	// TODO extract args.InnerPath path in the archive srcObj to the dstDir location, optional
	// a folder with the same name as the archive file needs to be created to store the extracted results if args.PutIntoNewDir
	// return errs.NotImplement to use an internal archive tool
	return nil, errs.NotImplement
}

//func (d *Doubao) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
//	return nil, errs.NotSupport
//}

var _ driver.Driver = (*Doubao)(nil)
