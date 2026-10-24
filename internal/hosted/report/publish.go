// Package report publishes commit-pinned static review bundles without
// executing pull-request code.
package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxBundleBytes int64 = 50 << 20

type ExportRequest struct{ Root, BaseSHA, HeadSHA string }

type Exporter interface {
	Export(context.Context, ExportRequest, string) error
}

// CLIExporter invokes the trusted px1 executable, not repository code.
type CLIExporter struct{ Binary string }

func (e CLIExporter) Export(ctx context.Context, request ExportRequest, outputDir string) error {
	binary := e.Binary
	if binary == "" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			return err
		}
	}
	command := exec.CommandContext(ctx, binary, "export-review", "--root", request.Root, "--base", request.BaseSHA, "--head", request.HeadSHA, "--out", outputDir)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("export static report: %w", err)
	}
	return nil
}

type Object struct {
	Key, ContentType, SHA256 string
	Body                     io.Reader
	Size                     int64
	Immutable                bool
}

type ObjectStore interface {
	Put(context.Context, Object) error
}

type Publisher struct {
	Exporter       Exporter
	Store          ObjectStore
	MaxBundleBytes int64
}

func (p Publisher) Publish(ctx context.Context, owner, repo string, request ExportRequest) ([]string, error) {
	if p.Exporter == nil || p.Store == nil || !validObjectSlug(owner) || !validObjectSlug(repo) || !fullSHA(request.HeadSHA) || !fullSHA(request.BaseSHA) || request.Root == "" {
		return nil, errors.New("exporter, store, repository, root, and full SHAs are required")
	}
	dir, err := os.MkdirTemp("", "px1-report-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := p.Exporter.Export(ctx, request, dir); err != nil {
		return nil, err
	}
	files := []struct{ name, contentType string }{{"index.html", "text/html; charset=utf-8"}, {"review.json", "application/json"}, {"checksums.sha256", "text/plain; charset=utf-8"}}
	limit := p.MaxBundleBytes
	if limit <= 0 {
		limit = maxBundleBytes
	}
	var total int64
	keys := make([]string, 0, len(files))
	objects := make([]Object, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("report output %s is missing or not regular", file.name)
		}
		total += info.Size()
		if total > limit {
			return nil, fmt.Errorf("report bundle exceeds %d bytes", limit)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		key := strings.Join([]string{"reports", owner, repo, request.HeadSHA, file.name}, "/")
		objects = append(objects, Object{Key: key, ContentType: file.contentType, SHA256: digest, Body: bytes.NewReader(body), Size: int64(len(body)), Immutable: true})
		keys = append(keys, key)
	}
	for i, object := range objects {
		if err := p.Store.Put(ctx, object); err != nil {
			return nil, fmt.Errorf("upload %s: %w", files[i].name, err)
		}
	}
	return keys, nil
}

func validObjectSlug(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
func fullSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
