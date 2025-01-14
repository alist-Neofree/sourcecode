package shadow

import (
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/sha3"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const NameHashLen = 12
const SplitPrefix = ".sd"
const MaxSplitNum = 0xff

var nameParseRe *regexp.Regexp

type NameInfo struct {
	Hash           string
	HashClashIndex int
	TotalSplit     int
	SplitIndex     int
	Name           string
}

func (info *NameInfo) ToFullName() string {
	// <SplitPrefix>.<hash>.<hash_clash_index>.<total_split>.<split_index>.<name>
	return fmt.Sprintf("%s.%s.%d.%d.%d.%s", SplitPrefix, info.Hash, info.HashClashIndex, info.TotalSplit, info.SplitIndex, info.Name)
}

func isSplitName(name string) bool {
	return strings.HasPrefix(name, SplitPrefix+".")
}

func SpongeHash(data []byte, length int) ([]byte, error) {
	hasher := sha3.NewShake128()
	_, err := hasher.Write(data)
	if err != nil {
		return nil, err
	}
	result := make([]byte, length)
	n, err := hasher.Read(result)
	if err != nil {
		return nil, err
	}
	return result[:n], nil
}

func GetFirstNAsciiChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}

func splitName(name string, maxNameLen int, hashClashIndex int) ([]string, error) {
	var names []string
	bName := base64.URLEncoding.EncodeToString([]byte(name))
	if len(bName) > maxNameLen {
		hash, err := SpongeHash([]byte(bName), NameHashLen)
		if err != nil {
			return nil, err
		}
		hashStr := base64.URLEncoding.EncodeToString(hash[:])
		hashFinal := GetFirstNAsciiChars(hashStr, NameHashLen)

		nameInfo := NameInfo{
			Hash:           hashFinal,
			HashClashIndex: hashClashIndex,
			TotalSplit:     MaxSplitNum,
			SplitIndex:     MaxSplitNum,
		}

		nameLen := maxNameLen - len(nameInfo.ToFullName())

		segments := SplitString(bName, nameLen)
		if len(segments) > MaxSplitNum {
			return nil, fmt.Errorf("split index is too long")
		}

		nameInfo.TotalSplit = len(segments)
		for i, segment := range segments {
			nameInfo.SplitIndex = i
			nameInfo.Name = segment
			names = append(names, nameInfo.ToFullName())
		}
	} else {
		names = append(names, bName)
	}
	return names, nil
}

func combineName(names []string) (string, error) {
	var infos []*NameInfo
	var fName string
	if len(names) == 0 {
		return "", errors.New("illegal params")
	}
	if len(names) == 1 && !isSplitName(names[0]) {
		fName = names[0]
	} else {
		for _, name := range names {
			if !isSplitName(name) {
				return "", fmt.Errorf("illegal decode name: %s", name)
			}
			info, err := parseSplitName(name)
			if err != nil {
				return "", err
			}
			infos = append(infos, info)
		}
		sort.Slice(infos, func(i, j int) bool {
			return infos[i].SplitIndex < infos[j].SplitIndex
		})
		var fullName []string
		for _, info := range infos {
			fullName = append(fullName, info.Name)
		}
		fName = strings.Join(fullName, "")
	}

	name, err := base64.URLEncoding.DecodeString(fName)
	if err != nil {
		return "", err
	}
	return string(name), nil
}

func parseSplitName(name string) (*NameInfo, error) {
	match := nameParseRe.FindStringSubmatch(name)
	if match == nil {
		return nil, fmt.Errorf("illegal split name: %s", name)
	}

	clashIndex, err := strconv.ParseInt(match[2], 16, 0)
	if err != nil {
		return nil, fmt.Errorf("illegal split name: %s", name)
	}

	totalSplit, err := strconv.ParseInt(match[3], 16, 0)
	if err != nil {
		return nil, fmt.Errorf("illegal split name: %s", name)
	}

	splitIndex, err := strconv.ParseInt(match[4], 16, 0)
	if err != nil {
		return nil, fmt.Errorf("illegal split name: %s", name)
	}

	return &NameInfo{
		Hash:           match[1],
		HashClashIndex: int(clashIndex),
		TotalSplit:     int(totalSplit),
		SplitIndex:     int(splitIndex),
		Name:           match[5],
	}, nil
}

func init() {
	// <SplitPrefix>.<hash>.<hash_clash_index>.<total_split>.<split_index>.<name>
	nameParseRe = regexp.MustCompile(SplitPrefix + `\.([^.]+)\.([^.]+)\.([^.]+)\.([^.]+)\.([^.]+)`)
}
