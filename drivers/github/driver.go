package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/go-resty/resty/v2"
	"io"
	stdpath "path"
	"strconv"
	"strings"
	"text/template"
)

type Github struct {
	model.Storage
	Addition
	client        *resty.Client
	mkdirMsgTmpl  *template.Template
	deleteMsgTmpl *template.Template
	putMsgTmpl    *template.Template
}

func (d *Github) Config() driver.Config {
	return config
}

func (d *Github) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Github) Init(ctx context.Context) error {
	d.RootFolderPath = utils.FixAndCleanPath(d.RootFolderPath)
	if d.CommitterName != "" && d.CommitterEmail == "" {
		return errors.New("committer email is required")
	}
	if d.CommitterName == "" && d.CommitterEmail != "" {
		return errors.New("committer name is required")
	}
	if d.AuthorName != "" && d.AuthorEmail == "" {
		return errors.New("author email is required")
	}
	if d.AuthorName == "" && d.AuthorEmail != "" {
		return errors.New("author name is required")
	}
	var err error
	d.mkdirMsgTmpl, err = template.New("mkdirCommitMsgTemplate").Parse(d.MkdirCommitMsg)
	if err != nil {
		return err
	}
	d.deleteMsgTmpl, err = template.New("deleteCommitMsgTemplate").Parse(d.DeleteCommitMsg)
	if err != nil {
		return err
	}
	d.putMsgTmpl, err = template.New("putCommitMsgTemplate").Parse(d.PutCommitMsg)
	if err != nil {
		return err
	}
	d.client = base.NewRestyClient().
		SetHeader("Accept", "application/vnd.github.object+json").
		SetHeader("Authorization", "Bearer "+d.Token).
		SetHeader("X-GitHub-Api-Version", "2022-11-28")
	return nil
}

func (d *Github) Drop(ctx context.Context) error {
	return nil
}

func (d *Github) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	obj, err := d.get(dir.GetPath())
	if err != nil {
		return nil, err
	}
	if obj.Entries == nil {
		return nil, errs.NotFolder
	}
	ret := make([]model.Obj, 0, len(obj.Entries))
	for _, entry := range obj.Entries {
		if entry.Name != ".gitkeep" {
			ret = append(ret, entry.toModelObj())
		}
	}
	return ret, nil
}

func (d *Github) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	obj, err := d.get(file.GetPath())
	if err != nil {
		return nil, err
	}
	return &model.Link{
		URL: obj.DownloadURL,
	}, nil
}

func (d *Github) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	commitMessage, err := getMessage(d.mkdirMsgTmpl, &MessageTemplateVars{
		UserName:   ctx.Value("user").(*model.User).Username,
		ObjName:    dirName,
		ObjPath:    stdpath.Join(parentDir.GetPath(), dirName),
		ParentName: parentDir.GetName(),
		ParentPath: parentDir.GetPath(),
	}, "mkdir")
	if err != nil {
		return err
	}
	parent, err := d.get(parentDir.GetPath())
	if err != nil {
		return err
	}
	if parent.Entries == nil {
		return errs.NotFolder
	}
	// if parent folder contains .gitkeep only, mark it and delete .gitkeep later
	gitKeepSha := ""
	if len(parent.Entries) == 1 && parent.Entries[0].Name == ".gitkeep" {
		gitKeepSha = parent.Entries[0].Sha
	}

	if err = d.createGitKeep(stdpath.Join(parentDir.GetPath(), dirName), commitMessage); err != nil {
		return err
	}
	if gitKeepSha != "" {
		err = d.delete(stdpath.Join(parentDir.GetPath(), ".gitkeep"), gitKeepSha, commitMessage)
	}
	return err
}

func (d *Github) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotSupport
}

func (d *Github) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	return nil, errs.NotSupport
}

func (d *Github) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotSupport
}

func (d *Github) Remove(ctx context.Context, obj model.Obj) error {
	parentDir := stdpath.Dir(obj.GetPath())
	commitMessage, err := getMessage(d.deleteMsgTmpl, &MessageTemplateVars{
		UserName:   ctx.Value("user").(*model.User).Username,
		ObjName:    obj.GetName(),
		ObjPath:    obj.GetPath(),
		ParentName: stdpath.Base(parentDir),
		ParentPath: parentDir,
	}, "remove")
	if err != nil {
		return err
	}
	parent, err := d.get(parentDir)
	if err != nil {
		return err
	}
	if parent.Entries == nil {
		return errs.ObjectNotFound
	}
	sha := ""
	isDir := false
	for _, entry := range parent.Entries {
		if entry.Name == obj.GetName() {
			sha = entry.Sha
			isDir = entry.Type == "dir"
			break
		}
	}
	if isDir {
		return d.rmdir(obj.GetPath(), commitMessage)
	}
	if sha == "" {
		return errs.ObjectNotFound
	}
	// if deleted file is the only child of its parent, create .gitkeep to retain empty folder
	if parentDir != "/" && len(parent.Entries) == 1 {
		if err = d.createGitKeep(parentDir, commitMessage); err != nil {
			return err
		}
	}
	return d.delete(obj.GetPath(), sha, commitMessage)
}

func (d *Github) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	commitMessage, err := getMessage(d.putMsgTmpl, &MessageTemplateVars{
		UserName:   ctx.Value("user").(*model.User).Username,
		ObjName:    stream.GetName(),
		ObjPath:    stdpath.Join(dstDir.GetPath(), stream.GetName()),
		ParentName: dstDir.GetName(),
		ParentPath: dstDir.GetPath(),
	}, "upload")
	if err != nil {
		return nil, err
	}
	parent, err := d.get(dstDir.GetPath())
	if err != nil {
		return nil, err
	}
	if parent.Entries == nil {
		return nil, errs.NotFolder
	}
	uploadSha := ""
	for _, entry := range parent.Entries {
		if entry.Name == stream.GetName() {
			uploadSha = entry.Sha
			break
		}
	}
	// if parent folder contains .gitkeep only, mark it and delete .gitkeep later
	gitKeepSha := ""
	if len(parent.Entries) == 1 && parent.Entries[0].Name == ".gitkeep" {
		gitKeepSha = parent.Entries[0].Sha
	}

	path := stdpath.Join(dstDir.GetPath(), stream.GetName())
	resp, err := d.put(path, uploadSha, commitMessage, ctx, stream, up)
	if err != nil {
		return nil, err
	}
	if gitKeepSha != "" {
		err = d.delete(stdpath.Join(dstDir.GetPath(), ".gitkeep"), gitKeepSha, commitMessage)
	}
	return resp.Content.toModelObj(), err
}

var _ driver.Driver = (*Github)(nil)

func (d *Github) getApiUrl(path string) string {
	path = utils.FixAndCleanPath(path)
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents%s", d.Owner, d.Repo, path)
}

func (d *Github) get(path string) (*Object, error) {
	req := d.client.R()
	if d.Ref != "" {
		req = req.SetQueryParam("ref", d.Ref)
	}
	res, err := req.Get(d.getApiUrl(path))
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 {
		return nil, toErr(res)
	}
	var resp Object
	err = utils.Json.Unmarshal(res.Body(), &resp)
	return &resp, err
}

func (d *Github) createGitKeep(path, message string) error {
	body := map[string]interface{}{
		"message": message,
		"content": "",
	}
	if d.Ref != "" {
		body["branch"] = d.Ref
	}
	d.addCommitterAndAuthor(&body)

	res, err := d.client.R().SetBody(body).Put(d.getApiUrl(stdpath.Join(path, ".gitkeep")))
	if err != nil {
		return err
	}
	if res.StatusCode() != 200 && res.StatusCode() != 201 {
		return toErr(res)
	}
	return nil
}

func (d *Github) put(path, sha, message string, ctx context.Context, stream model.FileStreamer, up driver.UpdateProgress) (*PutResp, error) {
	beforeContent := strings.Builder{}
	beforeContent.WriteString("{\"message\":\"")
	beforeContent.WriteString(message)
	beforeContent.WriteString("\",")
	if sha != "" {
		beforeContent.WriteString("\"sha\":\"")
		beforeContent.WriteString(sha)
		beforeContent.WriteString("\",")
	}
	if d.Ref != "" {
		beforeContent.WriteString("\"branch\":\"")
		beforeContent.WriteString(d.Ref)
		beforeContent.WriteString("\",")
	}
	if d.CommitterName != "" {
		beforeContent.WriteString("\"committer\":{\"name\":\"")
		beforeContent.WriteString(d.CommitterName)
		beforeContent.WriteString("\",\"email\":\"")
		beforeContent.WriteString(d.CommitterEmail)
		beforeContent.WriteString("\"},")
	}
	if d.AuthorName != "" {
		beforeContent.WriteString("\"author\":{\"name\":\"")
		beforeContent.WriteString(d.AuthorName)
		beforeContent.WriteString("\",\"email\":\"")
		beforeContent.WriteString(d.AuthorEmail)
		beforeContent.WriteString("\"},")
	}
	beforeContent.WriteString("\"content\":\"")

	length := int64(beforeContent.Len()) + calculateBase64Length(stream.GetSize()) + 2
	beforeContentReader := strings.NewReader(beforeContent.String())
	contentReader, contentWriter := io.Pipe()
	go func() {
		encoder := base64.NewEncoder(base64.StdEncoding, contentWriter)
		if _, err := io.Copy(encoder, stream); err != nil {
			_ = contentWriter.CloseWithError(err)
			return
		}
		_ = encoder.Close()
		_ = contentWriter.Close()
	}()
	afterContentReader := strings.NewReader("\"}")
	res, err := d.client.R().
		SetHeader("Content-Length", strconv.FormatInt(length, 10)).
		SetBody(&ReaderWithCtx{
			Reader:   io.MultiReader(beforeContentReader, contentReader, afterContentReader),
			Ctx:      ctx,
			Length:   length,
			Progress: up,
		}).
		Put(d.getApiUrl(path))
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 && res.StatusCode() != 201 {
		return nil, toErr(res)
	}
	var resp PutResp
	if err = utils.Json.Unmarshal(res.Body(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (d *Github) delete(path, sha, message string) error {
	body := map[string]interface{}{
		"message": message,
		"sha":     sha,
	}
	if d.Ref != "" {
		body["branch"] = d.Ref
	}
	d.addCommitterAndAuthor(&body)
	res, err := d.client.R().SetBody(body).Delete(d.getApiUrl(path))
	if err != nil {
		return err
	}
	if res.StatusCode() != 200 {
		return toErr(res)
	}
	return nil
}

func (d *Github) rmdir(path, message string) error {
	for { // the number of sub-items returned pre call is limited to a maximum of 1000
		obj, err := d.get(path)
		if err != nil { // until 404
			return nil
		}
		if obj.Type != "dir" || obj.Entries == nil {
			return errs.NotFolder
		}
		if len(obj.Entries) == 0 { // maybe never access
			return nil
		}
		if err = d.clearSub(obj, path, message); err != nil {
			return err
		}
	}
}

func (d *Github) clearSub(obj *Object, path, message string) error {
	for _, entry := range obj.Entries {
		var err error
		if entry.Type == "dir" {
			err = d.rmdir(stdpath.Join(path, entry.Name), message)
		} else {
			err = d.delete(stdpath.Join(path, entry.Name), entry.Sha, message)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Github) addCommitterAndAuthor(m *map[string]interface{}) {
	if d.CommitterName != "" {
		committer := map[string]string{
			"name":  d.CommitterName,
			"email": d.CommitterEmail,
		}
		(*m)["committer"] = committer
	}
	if d.AuthorName != "" {
		author := map[string]string{
			"name":  d.AuthorName,
			"email": d.AuthorEmail,
		}
		(*m)["author"] = author
	}
}
