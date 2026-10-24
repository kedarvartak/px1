package report

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type exporterStub struct{}

func (exporterStub) Export(_ context.Context, _ ExportRequest, dir string) error {
	for name, body := range map[string]string{"index.html": "<html></html>", "review.json": "{}", "checksums.sha256": "abc  index.html\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			return err
		}
	}
	return nil
}

type storeStub struct {
	objects []Object
	bodies  []string
}

func (s *storeStub) Put(_ context.Context, object Object) error {
	body, _ := io.ReadAll(object.Body)
	s.objects = append(s.objects, object)
	s.bodies = append(s.bodies, string(body))
	return nil
}

func TestPublisherUploadsImmutableCommitPinnedBundle(t *testing.T) {
	store := &storeStub{}
	sha := strings.Repeat("a", 40)
	keys, err := (Publisher{Exporter: exporterStub{}, Store: store}).Publish(context.Background(), "acme", "widget", ExportRequest{Root: "/repo", BaseSHA: strings.Repeat("b", 40), HeadSHA: sha})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || len(store.objects) != 3 {
		t.Fatalf("keys = %#v", keys)
	}
	for _, object := range store.objects {
		if !object.Immutable || !strings.HasPrefix(object.Key, "reports/acme/widget/"+sha+"/") || len(object.SHA256) != 64 {
			t.Fatalf("object = %#v", object)
		}
	}
}

func TestPublisherRequiresCompleteBundle(t *testing.T) {
	exporter := ExporterFunc(func(_ context.Context, _ ExportRequest, dir string) error {
		return os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0600)
	})
	_, err := (Publisher{Exporter: exporter, Store: &storeStub{}}).Publish(context.Background(), "acme", "widget", ExportRequest{Root: "/repo", BaseSHA: strings.Repeat("b", 40), HeadSHA: strings.Repeat("a", 40)})
	if err == nil {
		t.Fatal("expected incomplete bundle to fail")
	}
}

type ExporterFunc func(context.Context, ExportRequest, string) error

func (f ExporterFunc) Export(ctx context.Context, request ExportRequest, dir string) error {
	return f(ctx, request, dir)
}
