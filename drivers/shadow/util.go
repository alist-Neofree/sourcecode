package shadow

import (
	stdpath "path"
	"strings"
)

//func (d *Shadow) getPathForRemote(path string, isFolder bool) (string, error) {
//	if isFolder && !strings.HasSuffix(path, "/") {
//		path = path + "/"
//	}
//
//	encodedPath, err := encodePath(path, d.MaxFilenameLen)
//	if err != nil {
//		return "", err
//	}
//	return stdpath.Join(d.RemotePath, encodedPath), nil
//}
//
//// actual path is used for internal only. any link for user should come from remoteFullPath
//func (d *Shadow) getActualPathForRemote(path string, isFolder bool) (string, error) {
//	remote, err := d.getPathForRemote(path, isFolder)
//	if err != nil {
//		return "", err
//	}
//	_, remoteActualPath, err := op.GetStorageAndActualPath(remote)
//	return remoteActualPath, err
//}

func SplitString(s string, n int) []string {
	var result []string
	runes := []rune(s)
	length := len(runes)
	for i := 0; i < length; i += n {
		end := i + n
		if end > length {
			end = length
		}
		result = append(result, string(runes[i:end]))
	}
	return result
}

func encodePath(path string, maxSegmentLen int) (string, error) {
	segments := strings.Split(path, "/")
	var paths []string
	for i, name := range segments {
		if segments[i] == "" {
			continue
		}
		encodeName, err := splitName(name, maxSegmentLen, 0)
		if err != nil {
			return "", err
		}
		paths = append(paths, encodeName[0])
	}
	p := strings.Join(paths, "/")
	if strings.HasPrefix(path, "/") {
		p = "/" + p
	}
	if strings.HasSuffix(path, "/") {
		p += "/"
	}
	return p, nil
}

func SplitTarget(name string) (string, string) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSuffix(name, "/")
	return stdpath.Split(stdpath.Clean(name))
}
