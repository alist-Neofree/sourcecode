package shadow

import (
	"github.com/alist-org/alist/v3/internal/model"
)

type NamedObj struct {
	model.Obj
	Name string
}

func NewNamedObj(name string, obj model.Obj) *NamedObj {
	return &NamedObj{
		Obj:  obj,
		Name: name,
	}
}

func (obj *NamedObj) GetName() string {
	return obj.Name
}

type WrapObj struct {
	Name *string
	Path *string
	model.Obj
}

func (obj *WrapObj) UnWrap() model.Obj {
	return obj.Obj
}

func (obj *WrapObj) GetName() string {
	if obj.Name != nil {
		return *obj.Name
	}
	return obj.Obj.GetName()
}

func (obj *WrapObj) GetPath() string {
	if obj.Path != nil {
		return *obj.Path
	}
	return obj.Obj.GetPath()
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
