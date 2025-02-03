package stream

import (
	"context"
	"github.com/alist-org/alist/v3/internal/model"
	"golang.org/x/time/rate"
	"io"
)

var (
	ClientDownloadLimit *rate.Limiter
	ClientUploadLimit   *rate.Limiter
	ServerDownloadLimit *rate.Limiter
	ServerUploadLimit   *rate.Limiter
)

type RateLimitReader struct {
	io.ReadCloser
	Limiter *rate.Limiter
	Ctx     context.Context
}

func (r RateLimitReader) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	if err != nil {
		return
	}
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}

type RateLimitWriter struct {
	io.WriteCloser
	Limiter *rate.Limiter
	Ctx     context.Context
}

func (w RateLimitWriter) Write(p []byte) (n int, err error) {
	n, err = w.WriteCloser.Write(p)
	if err != nil {
		return
	}
	if w.Limiter != nil {
		if w.Ctx == nil {
			w.Ctx = context.Background()
		}
		err = w.Limiter.WaitN(w.Ctx, n)
	}
	return
}

type RateLimitFile struct {
	model.File
	Limiter *rate.Limiter
	Ctx     context.Context
}

func (r RateLimitFile) Read(p []byte) (n int, err error) {
	n, err = r.File.Read(p)
	if err != nil {
		return
	}
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}

func (r RateLimitFile) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = r.File.ReadAt(p, off)
	if err != nil {
		return
	}
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}
