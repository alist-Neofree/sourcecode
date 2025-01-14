package shadow

import (
	"bytes"
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	stdpath "path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
)

var IndexPlaceholderContent = []byte("_shadow_")

type Shadow struct {
	model.Storage
	Addition
	remoteStorage driver.Driver
}

func (d *Shadow) Config() driver.Config {
	return config
}

func (d *Shadow) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Shadow) Init(ctx context.Context) error {
	//need remote storage exist
	storage, err := fs.GetStorage(d.RemotePath, &fs.GetStoragesArgs{})
	if err != nil {
		return fmt.Errorf("can't find remote storage: %w", err)
	}
	d.remoteStorage = storage

	return nil
}

func (d *Shadow) Drop(ctx context.Context) error {
	return nil
}

func (d *Shadow) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	path := dir.GetPath()

	remotePath, err := d.getPathForRemote(path, true)
	if err != nil {
		return nil, err
	}
	objs, err := fs.List(ctx, remotePath, &fs.ListArgs{NoLog: true})
	if err != nil {
		return nil, err
	}

	//
	var normalObjs []*WrapObj
	splitNames := make(map[string][]string)
	splitObjs := make(map[string]model.Obj)
	for _, obj := range objs {
		if isSplitName(obj.GetName()) {
			nameInfo, err := parseSplitName(obj.GetName())
			if err != nil {
				log.Errorf("[shadow] parse split name fail: %s", err.Error())
				continue
			}
			key := fmt.Sprintf("%s_%d", nameInfo.Hash, nameInfo.HashClashIndex)
			splitNames[key] = append(splitNames[key], obj.GetName())
			if nameInfo.SplitIndex == 0 {
				splitObjs[key] = obj
			}
		} else {
			name, err := combineName([]string{obj.GetName()})
			if err != nil {
				continue
			}
			normalObjs = append(normalObjs, &WrapObj{Obj: obj, Name: &name})
		}
	}

	for k, v := range splitNames {
		name, err := combineName(v)
		if err != nil {
			log.Errorf("[shadow] decode split name fail: %s", err.Error())
			continue
		}
		if obj, ok := splitObjs[k]; ok {
			normalObjs = append(normalObjs, &WrapObj{Obj: obj, Name: &name})
		}
	}

	var result []model.Obj
	for _, obj := range normalObjs {
		if obj.IsDir() {
			if !d.ShowHidden && strings.HasPrefix(obj.GetName(), ".") {
				continue
			}
			result = append(result, obj)
		} else {
			thumb, ok := model.GetThumb(obj.UnWrap())
			if !d.ShowHidden && strings.HasPrefix(obj.GetName(), ".") {
				continue
			}
			if d.Thumbnail && thumb == "" {
				thumbPath := stdpath.Join(args.ReqPath, ".thumbnails", obj.GetName()+".webp")
				thumb = fmt.Sprintf("%s/d%s?sign=%s",
					common.GetApiUrl(common.GetHttpReq(ctx)),
					utils.EncodePath(thumbPath, true),
					sign.Sign(thumbPath))
			}
			if !ok && !d.Thumbnail {
				result = append(result, obj)
			} else {
				objWithThumb := model.ObjThumb{
					Object: *obj.GetObject(),
					Thumbnail: model.Thumbnail{
						Thumbnail: thumb,
					},
				}
				result = append(result, &objWithThumb)
			}
		}
	}

	return result, nil
}

func (d *Shadow) Get(ctx context.Context, path string) (model.Obj, error) {
	if utils.PathEqual(path, "/") {
		return &model.Object{
			Name:     "Root",
			IsFolder: true,
			Path:     "/",
		}, nil
	}

	remoteFullPath, err := d.getPathForRemote(path, false)
	if err != nil {
		return nil, err
	}
	remoteObj, err := fs.Get(ctx, remoteFullPath, &fs.GetArgs{NoLog: true})
	if err != nil {
		return nil, err
	}

	//remoteFullPath := ""
	//var remoteObj model.Obj
	//var err, err2 error
	//firstTryIsFolder, secondTry := guessPath(path)
	//remoteFullPath, err = d.getPathForRemote(path, firstTryIsFolder)
	//if err != nil {
	//	return nil, err
	//}
	//remoteObj, err = fs.Get(ctx, remoteFullPath, &fs.GetArgs{NoLog: true})
	//if err != nil {
	//	if errs.IsObjectNotFound(err) && secondTry {
	//		//try the opposite
	//		remoteFullPath, err2 = d.getPathForRemote(path, !firstTryIsFolder)
	//		if err2 != nil {
	//			return nil, err2
	//		}
	//		remoteObj, err2 = fs.Get(ctx, remoteFullPath, &fs.GetArgs{NoLog: true})
	//		if err2 != nil {
	//			return nil, err2
	//		}
	//	} else {
	//		return nil, err
	//	}
	//}
	_, name := filepath.Split(strings.TrimSuffix(path, "/"))

	obj := &WrapObj{Obj: remoteObj, Name: &name, Path: &path}
	return obj, nil
}

func (d *Shadow) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	dstDirActualPath, err := d.getPathForRemote(file.GetPath(), false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert path to remote path: %w", err)
	}

	link, _, err := fs.Link(ctx, dstDirActualPath, args)
	return link, err
}

func (d *Shadow) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	dstDirActualPath, err := d.getActualPathForRemote(parentDir.GetPath(), true)
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dirs, err := splitName(dirName, d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}

	group := sync.WaitGroup{}
	var errFinal error
	for _, dir := range dirs {
		group.Add(1)
		go func() {
			defer group.Done()
			err := op.MakeDir(ctx, d.remoteStorage, stdpath.Join(dstDirActualPath, dir))
			if err != nil {
				errFinal = err
			}
		}()
	}
	group.Wait()
	if errFinal != nil {
		for _, dir := range dirs {
			_ = op.Remove(ctx, d.remoteStorage, dir)
		}
	}
	return errFinal
}

// Move TODO:fit
func (d *Shadow) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcRemoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dstRemoteActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), dstDir.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return op.Move(ctx, d.remoteStorage, srcRemoteActualPath, dstRemoteActualPath)
}

// Rename TODO:fit
func (d *Shadow) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	remoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	encodedNames, err := splitName(newName, d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}
	return op.Rename(ctx, d.remoteStorage, remoteActualPath, encodedNames[0])
}

// Copy TODO:fit
func (d *Shadow) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {

	srcRemoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dstRemoteActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), dstDir.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return op.Copy(ctx, d.remoteStorage, srcRemoteActualPath, dstRemoteActualPath)
}

func (d *Shadow) Remove(ctx context.Context, obj model.Obj) error {
	remotePath, err := d.getPathForRemote(obj.GetPath(), obj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}

	return fs.Remove(ctx, remotePath)
}

func (d *Shadow) Put(ctx context.Context, dstDir model.Obj, streamer model.FileStreamer, up driver.UpdateProgress) error {
	dstDirActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), true)
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}

	encodedNames, err := splitName(streamer.GetName(), d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}

	group := sync.WaitGroup{}
	var errFinal error
	for i, name := range encodedNames {
		group.Add(1)
		go func() {
			var reader io.Reader
			var mimeType string
			if i == 0 {
				reader = streamer
				mimeType = streamer.GetMimetype()
			} else {
				reader = bytes.NewReader(IndexPlaceholderContent)
				mimeType = "text/plain"
			}
			streamOut := &stream.FileStream{
				Obj:               NewNamedObj(name, dstDir),
				Reader:            reader,
				Mimetype:          mimeType,
				WebPutAsTask:      streamer.NeedStore(),
				ForceStreamUpload: true,
				Exist:             streamer.GetExist(),
			}
			err = op.Put(ctx, d.remoteStorage, dstDirActualPath, streamOut, up, false)
			if err != nil {
				errFinal = err
			}
		}()
	}
	group.Wait()
	if errFinal != nil {
		for _, name := range encodedNames {
			_ = op.Remove(ctx, d.remoteStorage, stdpath.Join(dstDirActualPath, name))
		}
	}

	return errFinal
}

var _ driver.Driver = (*Shadow)(nil)
