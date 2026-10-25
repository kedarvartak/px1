package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const maxAddressedFileBytes = 4 << 20

type RevisionFileReader interface {
	ReadFile(context.Context, int64, int64, string, string) ([]byte, bool, error)
}

type AddressedInput struct {
	InstallationID int64
	RepositoryID   int64
	ReviewSHA      string
	MergeSHA       string
	Path           string
	StartLine      int
	EndLine        int
}

type AddressedResult struct {
	Addressed           bool
	Reason              string
	OriginalFingerprint string
}

// DetectAddressed compares the exact reviewed lines with the merge revision.
// It searches the whole merged file so unrelated insertions above the range do
// not create a false addressed signal.
func DetectAddressed(ctx context.Context, reader RevisionFileReader, input AddressedInput) (AddressedResult, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(input.Path))
	if reader == nil || input.InstallationID <= 0 || input.RepositoryID <= 0 || !fullSHA(input.ReviewSHA) || !fullSHA(input.MergeSHA) || input.ReviewSHA == input.MergeSHA || input.Path == "" || filepath.IsAbs(input.Path) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || input.StartLine <= 0 || input.EndLine < input.StartLine {
		return AddressedResult{}, errors.New("tenant, distinct revisions, and a valid reviewed range are required")
	}
	original, exists, err := reader.ReadFile(ctx, input.InstallationID, input.RepositoryID, input.ReviewSHA, cleanPath)
	if err != nil || !exists {
		if err == nil {
			err = errors.New("reviewed file does not exist at review SHA")
		}
		return AddressedResult{}, fmt.Errorf("read reviewed file: %w", err)
	}
	if len(original) > maxAddressedFileBytes {
		return AddressedResult{}, errors.New("reviewed file exceeds addressed-detection limit")
	}
	lines := splitReviewLines(original)
	if input.EndLine > len(lines) {
		return AddressedResult{}, errors.New("reviewed range exceeds file length")
	}
	wanted := lines[input.StartLine-1 : input.EndLine]
	snippet := bytes.Join(wanted, []byte("\n"))
	digest := sha256.Sum256(snippet)
	result := AddressedResult{OriginalFingerprint: hex.EncodeToString(digest[:])}
	merged, exists, err := reader.ReadFile(ctx, input.InstallationID, input.RepositoryID, input.MergeSHA, cleanPath)
	if err != nil {
		return AddressedResult{}, fmt.Errorf("read merged file: %w", err)
	}
	if !exists {
		result.Addressed, result.Reason = true, "file was deleted before merge"
		return result, nil
	}
	if len(merged) > maxAddressedFileBytes {
		return AddressedResult{}, errors.New("merged file exceeds addressed-detection limit")
	}
	if containsLineSequence(splitReviewLines(merged), wanted) {
		result.Reason = "reviewed lines remain unchanged at merge"
		return result, nil
	}
	result.Addressed, result.Reason = true, "reviewed lines changed before merge"
	return result, nil
}

func splitReviewLines(content []byte) [][]byte {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.Split(bytes.TrimSuffix(content, []byte("\n")), []byte("\n"))
}

func containsLineSequence(lines, wanted [][]byte) bool {
	if len(wanted) == 0 || len(wanted) > len(lines) {
		return false
	}
	for i := 0; i+len(wanted) <= len(lines); i++ {
		matched := true
		for offset := range wanted {
			if !bytes.Equal(lines[i+offset], wanted[offset]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
