package shadow

import (
	"github.com/alist-org/alist/v3/internal/model"
)

type WrapObj struct {
	model.Obj
	Name        string
	RemotePaths []string
}

func MustWrapObj(obj model.Obj) *WrapObj {
	if w, ok := obj.(*WrapObj); ok {
		return w
	} else {
		panic("can not convert to wrap obj")
	}
}

func (obj *WrapObj) UnWrap() model.Obj {
	return obj.Obj
}

func (obj *WrapObj) GetName() string {
	return obj.Name
}

func (obj *WrapObj) GetPath() string {
	return obj.RemotePaths[0]
}

func (obj *WrapObj) GetRemotePaths() []string {
	return obj.RemotePaths
}

func (obj *WrapObj) GetObject() *model.Object {
	return &model.Object{
		ID:       obj.GetID(),
		Path:     obj.GetPath(),
		Name:     obj.GetName(),
		Size:     obj.GetSize(),
		Modified: obj.ModTime(),
		Ctime:    obj.CreateTime(),
		IsFolder: obj.IsDir(),
		HashInfo: obj.GetHash(),
	}
}

type WrapNameStreamer struct {
	model.FileStreamer
	Name string
}

func (w *WrapNameStreamer) GetName() string {
	return w.Name
}
