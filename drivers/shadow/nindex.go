package shadow

import (
	"bytes"
	"context"
	"fmt"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var IndexContentMagic = []byte("nindex")

type NameIndex struct {
	path   string
	prefix string
	//
	id atomic.Uint64
}

func NewNameIndex(path string, prefix string) *NameIndex {
	return &NameIndex{
		path:   path,
		prefix: prefix,
	}
}

func (index *NameIndex) Init(ctx context.Context) error {
	if _, err := fs.Get(ctx, index.path, &fs.GetArgs{NoLog: true}); err != nil {
		if err = fs.MakeDir(ctx, index.path); err != nil {
			return err
		}
	}

	objs, err := fs.List(ctx, index.path, &fs.ListArgs{NoLog: true})
	if err != nil {
		return err
	}

	var maxId uint64 = 0
	for _, obj := range objs {
		if strings.HasPrefix(obj.GetName(), index.prefix) {
			id, err := strconv.ParseUint(strings.TrimPrefix(obj.GetName(), index.prefix), 10, 64)
			if err != nil {
				return err
			}
			maxId = max(maxId, id)
		}
	}
	index.id.Store(maxId)

	maxIndexName := fmt.Sprintf("%s%d", index.prefix, maxId)
	for _, obj := range objs {
		if strings.HasPrefix(obj.GetName(), index.prefix) && obj.GetName() != maxIndexName {
			go fs.Remove(ctx, path.Join(index.path, obj.GetName()))
		}
	}
	return nil
}

func (index *NameIndex) Next(ctx context.Context) (uint64, error) {
	id := index.id.Add(1)

	name := fmt.Sprintf("%s%d", index.prefix, id)
	prevName := fmt.Sprintf("%s%d", index.prefix, id-1)
	if id == 1 {
		prevName = ""
	}
	now := time.Now()
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     int64(len(IndexContentMagic)),
			Modified: now,
		},
		Reader:       bytes.NewReader(IndexContentMagic),
		Mimetype:     "text/plain",
		WebPutAsTask: false,
	}

	err := fs.PutDirectly(ctx, index.path, s, true)
	if err != nil {
		return 0, err
	}

	if prevName != "" {
		go fs.Remove(ctx, path.Join(index.path, prevName))
	}
	return id, nil
}
