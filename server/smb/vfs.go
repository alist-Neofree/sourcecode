package smb

import (
	"context"
	"errors"
	"io"
	"os"
	stdpath "path"
	"sync"
	"sync/atomic"

	"github.com/KirCute/go-smb2-alist/vfs"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/stream"
)

type VFS struct {
	ctx           context.Context
	openedPath    sync.Map // VfsHandle: *readingFile
	openedTmpFile sync.Map // VfsHandle: *writingFile
	nextHandle    atomic.Uint64
}

func NewVFS(ctx context.Context) *VFS {
	return &VFS{
		ctx:           ctx,
		openedPath:    sync.Map{},
		openedTmpFile: sync.Map{},
		nextHandle:    atomic.Uint64{},
	}
}

func (fs *VFS) GetAttr(handle vfs.VfsHandle) (*vfs.Attributes, error) {
	if tmp, ok := fs.openedTmpFile.Load(handle); ok {
		return MakeFileAttribute(tmp.(*writingFile).f)
	}
	if r, ok := fs.openedPath.Load(handle); ok {
		return MakeObjAttribute(r.(*readingFile).obj)
	}
	return nil, errors.New("bad handle")
}

func (fs *VFS) Flush(handle vfs.VfsHandle) error {
	if tmp, ok := fs.openedTmpFile.Load(handle); ok {
		return tmp.(*writingFile).f.Sync()
	}
	return nil
}

func (fs *VFS) newUpload(path string) (vfs.VfsHandle, error) {
	f, err := newUpload(fs.ctx, path)
	if err != nil {
		return 0, err
	}
	handle := vfs.VfsHandle(fs.nextHandle.Add(1))
	fs.openedTmpFile.Store(handle, f)
	return handle, nil
}

func (fs *VFS) Open(path string, flags int, _ int) (vfs.VfsHandle, error) {
	if (flags & os.O_APPEND) != 0 {
		return 0, errs.NotSupport
	}
	obj, err := FsGet(fs.ctx, path)
	if errors.Is(err, errs.ObjectNotFound) {
		if (flags&os.O_RDWR) == 0 || (flags&os.O_CREATE) == 0 {
			return 0, os.ErrNotExist
		}
		return fs.newUpload(path)
	} else if err == nil {
		if (flags&os.O_CREATE) != 0 && (flags&os.O_EXCL) != 0 {
			return 0, os.ErrExist
		}
		if (flags & os.O_RDWR) != 0 {
			if (flags&os.O_CREATE) != 0 && (flags&os.O_TRUNC) != 0 {
				return fs.newUpload(path)
			}
			return 0, errs.NotSupport
		}
		handle := vfs.VfsHandle(fs.nextHandle.Add(1))
		fs.openedPath.Store(handle, newRead(path, obj))
		return handle, nil
	}
	return 0, err
}

func (fs *VFS) Close(handle vfs.VfsHandle) error {
	if tmp, ok := fs.openedTmpFile.Load(handle); ok {
		err := tmp.(*writingFile).close(fs.ctx)
		fs.openedTmpFile.Delete(handle)
		return err
	}
	if r, ok := fs.openedPath.Load(handle); ok {
		err := r.(*readingFile).Close()
		fs.openedPath.Delete(handle)
		return err
	}
	return errors.New("bad handle")
}

func (fs *VFS) Lookup(handle vfs.VfsHandle, name string) (*vfs.Attributes, error) {
	var p *readingFile
	if r, ok := fs.openedPath.Load(handle); ok {
		p = r.(*readingFile)
	} else {
		return nil, errors.New("bad handle")
	}
	obj, err := FsGet(fs.ctx, stdpath.Join(p.path, name))
	if err != nil {
		return nil, err
	}
	return MakeObjAttribute(obj)
}

func (fs *VFS) Mkdir(path string, _ int) (*vfs.Attributes, error) {
	obj, err := Mkdir(fs.ctx, path)
	if err != nil {
		return nil, err
	}
	return MakeObjAttribute(obj)
}

func (fs *VFS) Read(handle vfs.VfsHandle, buf []byte, offset uint64, _ int) (n int, err error) {
	if tmp, ok := fs.openedTmpFile.Load(handle); ok {
		n, err = tmp.(*writingFile).f.ReadAt(buf, int64(offset))
	} else if r, ok := fs.openedPath.Load(handle); ok {
		rf := r.(*readingFile)
		err = rf.initDownload(fs.ctx)
		if err == nil {
			n, err = rf.s.ReadAt(buf, int64(offset))
		}
	} else {
		err = errors.New("bad handle")
	}
	_ = stream.ClientDownloadLimit.WaitN(fs.ctx, n)
	return
}

func (fs *VFS) Write(handle vfs.VfsHandle, buf []byte, offset uint64, _ int) (n int, err error) {
	if tmp, ok := fs.openedTmpFile.Load(handle); ok {
		n, err = tmp.(*writingFile).f.WriteAt(buf, int64(offset))
	} else {
		err = errors.New("bad handle")
	}
	_ = stream.ClientUploadLimit.WaitN(fs.ctx, n)
	return
}

func (fs *VFS) OpenDir(path string) (vfs.VfsHandle, error) {
	obj, err := FsGet(fs.ctx, path)
	if err != nil {
		return 0, err
	}
	handle := vfs.VfsHandle(fs.nextHandle.Add(1))
	fs.openedPath.Store(handle, newRead(path, obj))
	return handle, nil
}

func (fs *VFS) ReadDir(handle vfs.VfsHandle, pos int, _ int) ([]vfs.DirInfo, error) {
	var p *readingFile
	if r, ok := fs.openedPath.Load(handle); ok {
		p = r.(*readingFile)
	} else {
		return nil, errors.New("bad handle")
	}
	if pos == 0 && p.dirRead {
		return nil, io.EOF
	}
	p.dirRead = true
	return List(fs.ctx, p.path)
}

func (fs *VFS) Unlink(handle vfs.VfsHandle) error {
	if r, ok := fs.openedPath.Load(handle); ok {
		rf := r.(*readingFile)
		_ = rf.Close()
		fs.openedPath.Delete(handle)
		return Remove(fs.ctx, rf.path)
	}
	return errors.New("bad handle")
}

func (fs *VFS) Rename(handle vfs.VfsHandle, to string, _ int) error {
	if r, ok := fs.openedPath.Load(handle); ok {
		rf := r.(*readingFile)
		_ = rf.Close()
		fs.openedPath.Delete(handle)
		return Rename(fs.ctx, rf.path, to)
	}
	return errors.New("bad handle")
}

func (fs *VFS) StatFS(vfs.VfsHandle) (*vfs.FSAttributes, error) {
	return &vfs.FSAttributes{}, nil
}

func (fs *VFS) SetAttr(vfs.VfsHandle, *vfs.Attributes) (*vfs.Attributes, error) {
	return nil, errs.NotSupport
}

func (fs *VFS) FSync(vfs.VfsHandle) error {
	return nil
}

func (fs *VFS) Readlink(vfs.VfsHandle) (string, error) {
	return "", errs.NotSupport
}

func (fs *VFS) Truncate(vfs.VfsHandle, uint64) error {
	return errs.NotSupport
}

func (fs *VFS) Symlink(vfs.VfsHandle, string, int) (*vfs.Attributes, error) {
	return nil, errs.NotSupport
}

func (fs *VFS) Link(vfs.VfsNode, vfs.VfsNode, string) (*vfs.Attributes, error) {
	return nil, nil
}

func (fs *VFS) Listxattr(vfs.VfsHandle) ([]string, error) {
	return []string{}, nil
}

func (fs *VFS) Getxattr(vfs.VfsHandle, string, []byte) (int, error) {
	return 0, errs.NotSupport
}

func (fs *VFS) Setxattr(vfs.VfsHandle, string, []byte) error {
	return errs.NotSupport
}

func (fs *VFS) Removexattr(vfs.VfsHandle, string) error {
	return errs.NotSupport
}
