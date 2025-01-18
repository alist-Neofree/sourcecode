package shadow

import (
	"bytes"
	"context"
	"fmt"
	"github.com/alist-org/alist/v3/pkg/errgroup"
	"github.com/avast/retry-go"
	log "github.com/sirupsen/logrus"
	stdpath "path"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
)

var IndexPlaceholderContent = []byte("_sd_")

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
	objs, err := fs.List(ctx, dir.GetPath(), &fs.ListArgs{NoLog: true})
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
			normalObjs = append(normalObjs, &WrapObj{
				Obj:         obj,
				Name:        name,
				RemotePaths: []string{stdpath.Join(dir.GetPath(), obj.GetName())},
			})
		}
	}

	for k, v := range splitNames {
		name, err := combineName(v)
		if err != nil {
			log.Errorf("[shadow] decode split name fail: %s", err.Error())
			continue
		}
		if obj, ok := splitObjs[k]; ok {
			names, err := splitName(name, d.MaxFilenameLen, 0)
			if err != nil {
				continue
			}
			for i, name_ := range names {
				names[i] = stdpath.Join(dir.GetPath(), name_)
			}
			normalObjs = append(normalObjs, &WrapObj{Obj: obj, Name: name, RemotePaths: names})
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
		return &WrapObj{
			Name:        "Root",
			RemotePaths: []string{stdpath.Clean(d.RemotePath)},
			Obj: &model.Object{
				Name:     "Root",
				IsFolder: true,
				Path:     "/",
			},
		}, nil
	}

	dir, name := SplitTarget(path)
	remoteDir, err := encodePath(dir, d.MaxFilenameLen)
	if err != nil {
		return nil, err
	}

	names, err := splitName(name, d.MaxFilenameLen, 0)
	if err != nil {
		return nil, err
	}
	for i, name_ := range names {
		names[i] = stdpath.Join(d.RemotePath, remoteDir, name_)
	}

	remoteObj, err := fs.Get(ctx, names[0], &fs.GetArgs{NoLog: true})
	if err != nil {
		return nil, err
	}

	obj := &WrapObj{Obj: remoteObj, Name: name, RemotePaths: names}
	return obj, nil
}

func (d *Shadow) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	link, _, err := fs.Link(ctx, file.GetPath(), args)
	return link, err
}

func (d *Shadow) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	dirs, err := splitName(dirName, d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}

	group, ctx2 := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	for _, dir := range dirs {
		if utils.IsCanceled(ctx2) {
			break
		}
		group.Go(func(ctx context.Context) error {
			return fs.MakeDir(ctx, stdpath.Join(parentDir.GetPath(), dir))
		})
	}

	if err = group.Wait(); err != nil {
		go func() {
			for _, dir := range dirs {
				_ = fs.Remove(context.Background(), stdpath.Join(parentDir.GetPath(), dir))
			}
		}()
		return err
	}
	return nil
}

func (d *Shadow) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	group, _ := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	for _, srcPath := range MustWrapObj(srcObj).GetRemotePaths() {
		srcPath := srcPath
		group.Go(func(ctx context.Context) error {
			return fs.Move(ctx, srcPath, dstDir.GetPath())
		})
	}

	return group.Wait()
}

func (d *Shadow) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	newNames, err := splitName(newName, d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}
	oldNames := MustWrapObj(srcObj).GetRemotePaths()

	group, _ := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	maxIt := max(len(newNames), len(oldNames))
	for i := 0; i < maxIt; i++ {
		i := i
		group.Go(func(ctx context.Context) error {
			if i < len(newNames) {
				newName1 := newNames[i]
				if i < len(oldNames) {
					oldName := oldNames[i]
					return fs.Rename(ctx, oldName, newName1)
				} else {
					remoteDir, _ := SplitTarget(srcObj.GetPath())
					return d.PutFile(ctx, newName1, IndexPlaceholderContent, remoteDir, "text/plain")
				}
			} else if i >= len(newNames) && i < len(oldNames) {
				oldName := oldNames[i]
				_ = fs.Remove(ctx, oldName)
			}
			return nil
		})
	}

	return group.Wait()
}

func (d *Shadow) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	group, ctx2 := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	var addedFiles []string
	for _, srcPath := range MustWrapObj(srcObj).GetRemotePaths() {
		if utils.IsCanceled(ctx2) {
			break
		}
		srcPath := srcPath
		_, name := SplitTarget(srcPath)
		addedFiles = append(addedFiles, stdpath.Join(dstDir.GetPath(), name))
		group.Go(func(ctx context.Context) error {
			_, err := fs.Copy(ctx, srcPath, dstDir.GetPath())
			return err
		})
	}

	if err := group.Wait(); err != nil {
		go func() {
			for _, delFile := range addedFiles {
				_ = fs.Remove(context.Background(), delFile)
			}
		}()
		return err
	}
	return nil
}

func (d *Shadow) Remove(ctx context.Context, obj model.Obj) error {
	group, _ := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	for _, name := range MustWrapObj(obj).GetRemotePaths() {
		name := name
		group.Go(func(ctx context.Context) error {
			return fs.Remove(ctx, name)
		})
	}

	return group.Wait()
}

func (d *Shadow) Put(ctx context.Context, dstDir model.Obj, streamer model.FileStreamer, up driver.UpdateProgress) error {
	encodedNames, err := splitName(streamer.GetName(), d.MaxFilenameLen, 0)
	if err != nil {
		return err
	}

	group, ctx2 := errgroup.NewGroupWithContext(ctx, 0, retry.Attempts(3))
	var addedFiles []string
	for i, name := range encodedNames {
		if utils.IsCanceled(ctx2) {
			break
		}
		i, name := i, name
		addedFiles = append(addedFiles, stdpath.Join(dstDir.GetPath(), name))
		group.Go(func(ctx context.Context) error {
			if i == 0 {
				storage, actualPath, err := op.GetStorageAndActualPath(dstDir.GetPath())
				if err != nil {
					return err
				}
				return op.Put(ctx, storage, actualPath, &WrapNameStreamer{
					FileStreamer: streamer,
					Name:         name,
				}, up, true)
			} else {
				return d.PutFile(ctx, name, IndexPlaceholderContent, dstDir.GetPath(), "text/plain")
			}
		})
	}

	if err = group.Wait(); err != nil {
		go func() {
			for _, delFile := range addedFiles {
				_ = fs.Remove(context.Background(), delFile)
			}
		}()
		return err
	}

	return nil
}

func (d *Shadow) PutFile(
	ctx context.Context,
	name string,
	data []byte,
	remoteFullDir string,
	mimeType string,
) error {
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     int64(len(data)),
			Modified: time.Now(),
		},
		Reader:       bytes.NewReader(data),
		Mimetype:     mimeType,
		WebPutAsTask: false,
	}
	return fs.PutDirectly(ctx, remoteFullDir, s, true)
}

var _ driver.Driver = (*Shadow)(nil)
