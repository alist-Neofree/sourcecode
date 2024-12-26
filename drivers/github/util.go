package github

import (
	"context"
	"errors"
	"fmt"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/go-resty/resty/v2"
	"io"
	"math"
	"strings"
	"text/template"
)

type ReaderWithCtx struct {
	Reader   io.Reader
	Ctx      context.Context
	Length   int64
	Progress func(percentage float64)
	offset   int64
}

func (r *ReaderWithCtx) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.offset += int64(n)
	r.Progress(math.Min(100.0, float64(r.offset)/float64(r.Length)))
	return n, err
}

type MessageTemplateVars struct {
	UserName   string
	ObjName    string
	ObjPath    string
	ParentName string
	ParentPath string
}

func getMessage(tmpl *template.Template, vars *MessageTemplateVars, defaultOpStr string) (string, error) {
	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, vars); err != nil {
		return fmt.Sprintf("%s %s %s", vars.UserName, defaultOpStr, vars.ObjPath), err
	}
	return sb.String(), nil
}

func calculateBase64Length(inputLength int64) int64 {
	return 4 * ((inputLength + 2) / 3)
}

func toErr(res *resty.Response) error {
	var errMsg ErrResp
	if err := utils.Json.Unmarshal(res.Body(), &errMsg); err != nil {
		return errors.New(res.Status())
	} else {
		return fmt.Errorf("%s: %s", res.Status(), errMsg.Message)
	}
}
