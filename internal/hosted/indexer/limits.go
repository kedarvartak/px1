package indexer

import (
	"errors"
	"fmt"
	"sort"
)

const (
	DefaultMaxFiles int64 = 50_000
	DefaultMaxBytes int64 = 2 << 30
)

type RepositoryLimits struct {
	MaxFiles int64
	MaxBytes int64
}

type LimitFile struct {
	Path string
	Size int64
}

type IndexLimitResult struct {
	State        string
	IndexedFiles []LimitFile
	SkippedFiles int
	IndexedBytes int64
	TotalFiles   int
	TotalBytes   int64
	Reason       string
}

func ApplyRepositoryLimits(files []LimitFile, limits RepositoryLimits) (IndexLimitResult, error) {
	maxFiles, maxBytes := limits.MaxFiles, limits.MaxBytes
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	sorted := append([]LimitFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	result := IndexLimitResult{State: "ready", TotalFiles: len(sorted)}
	for _, file := range sorted {
		if file.Path == "" || file.Size < 0 {
			return IndexLimitResult{}, errors.New("limit inventory requires paths and non-negative sizes")
		}
		result.TotalBytes += file.Size
		if int64(len(result.IndexedFiles)) >= maxFiles || result.IndexedBytes+file.Size > maxBytes {
			result.SkippedFiles++
			continue
		}
		result.IndexedFiles = append(result.IndexedFiles, file)
		result.IndexedBytes += file.Size
	}
	if result.SkippedFiles > 0 {
		result.State = "partial"
		result.Reason = fmt.Sprintf("indexed %d of %d files and %d of %d bytes; repository limit reached", len(result.IndexedFiles), result.TotalFiles, result.IndexedBytes, result.TotalBytes)
	}
	return result, nil
}

func (r IndexLimitResult) SummaryLine() string {
	if r.State == "partial" {
		return "Repository context is partial: " + r.Reason + ". Findings may miss skipped code."
	}
	return fmt.Sprintf("Repository context is ready: %d files (%d bytes) indexed.", len(r.IndexedFiles), r.IndexedBytes)
}
