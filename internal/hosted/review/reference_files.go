package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const maxReferenceFileBytes = 64 << 10

// ReferenceFiles is the bounded .px1/files.json contract. References add
// reviewer context; they never authorize reading outside the checked-out repo.
type ReferenceFiles struct {
	Version int             `json:"version"`
	Files   []ReferenceFile `json:"files"`
}

type ReferenceFile struct {
	Path        string   `json:"path"`
	For         []string `json:"for"`
	Description string   `json:"description,omitempty"`
}

func DecodeReferenceFiles(data []byte) (ReferenceFiles, error) {
	if len(data) > maxReferenceFileBytes {
		return ReferenceFiles{}, errors.New("files.json exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config ReferenceFiles
	if err := decoder.Decode(&config); err != nil {
		return ReferenceFiles{}, fmt.Errorf("decode files.json: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ReferenceFiles{}, errors.New("files.json contains trailing data")
	}
	if config.Version != 1 || len(config.Files) > 100 {
		return ReferenceFiles{}, errors.New("files.json requires version 1 and at most 100 files")
	}
	seen := map[string]bool{}
	for i, file := range config.Files {
		if !safeRepoPath(file.Path) || seen[file.Path] || len(file.For) == 0 || len(file.For) > 20 || len(file.Description) > 500 {
			return ReferenceFiles{}, fmt.Errorf("files.json entry %d is invalid", i)
		}
		seen[file.Path] = true
		for _, pattern := range file.For {
			if !safePattern(pattern) {
				return ReferenceFiles{}, fmt.Errorf("files.json entry %d has invalid scope", i)
			}
		}
	}
	return config, nil
}

func (c ReferenceFiles) ForPath(changedPath string) []ReferenceFile {
	if !safeRepoPath(changedPath) {
		return nil
	}
	var result []ReferenceFile
	for _, file := range c.Files {
		if !safeRepoPath(file.Path) {
			continue
		}
		for _, pattern := range file.For {
			if globMatch(pattern, changedPath) {
				result = append(result, file)
				break
			}
		}
	}
	return result
}

func safeRepoPath(value string) bool {
	return value != "" && value == path.Clean(value) && value != "." && !strings.HasPrefix(value, "/") && value != ".." && !strings.HasPrefix(value, "../")
}

func safePattern(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "..") || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	_, err := path.Match(strings.ReplaceAll(value, "**", "*"), "probe")
	return err == nil
}

func globMatch(pattern, value string) bool {
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "**"))
	}
	matched, _ := path.Match(pattern, value)
	return matched
}
