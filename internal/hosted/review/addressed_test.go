package review

import (
	"context"
	"strings"
	"testing"
)

type revisionReaderFunc func(context.Context, int64, int64, string, string) ([]byte, bool, error)

func (f revisionReaderFunc) ReadFile(ctx context.Context, installationID, repositoryID int64, sha, path string) ([]byte, bool, error) {
	return f(ctx, installationID, repositoryID, sha, path)
}

func TestDetectAddressedIgnoresLineShiftButDetectsEdit(t *testing.T) {
	reviewSHA, mergeSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	merged := "new header\none\nflagged line\nthree\n"
	reader := revisionReaderFunc(func(_ context.Context, installationID, repositoryID int64, sha, path string) ([]byte, bool, error) {
		if installationID != 1 || repositoryID != 2 || path != "a.go" {
			t.Fatal("lost tenant scope")
		}
		if sha == reviewSHA {
			return []byte("one\nflagged line\nthree\n"), true, nil
		}
		return []byte(merged), true, nil
	})
	result, err := DetectAddressed(context.Background(), reader, AddressedInput{InstallationID: 1, RepositoryID: 2, ReviewSHA: reviewSHA, MergeSHA: mergeSHA, Path: "a.go", StartLine: 2, EndLine: 2})
	if err != nil || result.Addressed {
		t.Fatalf("line shift counted as addressed: %+v %v", result, err)
	}
	merged = "new header\none\nfixed line\nthree\n"
	result, err = DetectAddressed(context.Background(), reader, AddressedInput{InstallationID: 1, RepositoryID: 2, ReviewSHA: reviewSHA, MergeSHA: mergeSHA, Path: "a.go", StartLine: 2, EndLine: 2})
	if err != nil || !result.Addressed || result.OriginalFingerprint == "" {
		t.Fatalf("edit was not addressed: %+v %v", result, err)
	}
}

func TestDetectAddressedTreatsDeletionAsAddressed(t *testing.T) {
	reviewSHA, mergeSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	reader := revisionReaderFunc(func(_ context.Context, _ int64, _ int64, sha, _ string) ([]byte, bool, error) {
		if sha == reviewSHA {
			return []byte("bad\n"), true, nil
		}
		return nil, false, nil
	})
	result, err := DetectAddressed(context.Background(), reader, AddressedInput{InstallationID: 1, RepositoryID: 2, ReviewSHA: reviewSHA, MergeSHA: mergeSHA, Path: "a.go", StartLine: 1, EndLine: 1})
	if err != nil || !result.Addressed {
		t.Fatalf("deletion = %+v, %v", result, err)
	}
}
