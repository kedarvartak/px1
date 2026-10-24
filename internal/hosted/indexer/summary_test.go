package indexer

import (
	"context"
	"strings"
	"testing"

	"px1/internal/hosted/llm"
)

type completionStub struct {
	requests []llm.Request
}

func (c *completionStub) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	c.requests = append(c.requests, request)
	return llm.Response{Text: "Routes requests and stores durable review state."}, nil
}

type summaryStoreStub struct{ records []FileSummary }

func (s *summaryStoreStub) UpsertFileSummary(_ context.Context, summary FileSummary) error {
	s.records = append(s.records, summary)
	return nil
}

func TestSummarizerCreatesVersionedSummaryForLongSource(t *testing.T) {
	client := &completionStub{}
	store := &summaryStoreStub{}
	summarizer := FileSummarizer{
		Client: client, Store: store, Provider: "openai", Model: "small-model",
		PromptVersion: "file-summary-v1", MaxSourceBytes: 80, MaxOutputTokens: 100,
	}
	file := SummaryFile{InstallationID: 1, RepositoryID: 2, Path: "service.go", Language: "go", Content: []byte(strings.Repeat("package line\n", 60))}
	result, err := summarizer.Summarize(context.Background(), file)
	if err != nil || !result.Created {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(client.requests) != 1 || client.requests[0].MaxOutputTokens != 100 {
		t.Fatalf("requests = %+v", client.requests)
	}
	if len(store.records) != 1 || store.records[0].PromptVersion != "file-summary-v1" || !store.records[0].SourceTruncated {
		t.Fatalf("records = %+v", store.records)
	}
}

func TestSummarizerSkipsShortGeneratedBinaryAndExcludedFiles(t *testing.T) {
	client := &completionStub{}
	summarizer := FileSummarizer{Client: client, Store: &summaryStoreStub{}, Provider: "openai", Model: "small", PromptVersion: "v1"}
	tests := []SummaryFile{
		{InstallationID: 1, RepositoryID: 2, Path: "short.go", Content: []byte("one\ntwo")},
		{InstallationID: 1, RepositoryID: 2, Path: "generated.go", Content: []byte(strings.Repeat("x\n", 60)), Generated: true},
		{InstallationID: 1, RepositoryID: 2, Path: "binary", Content: []byte{'a', 0, 'b'}},
		{InstallationID: 1, RepositoryID: 2, Path: "secret.go", Content: []byte(strings.Repeat("x\n", 60)), ModelExcluded: true},
	}
	for _, file := range tests {
		result, err := summarizer.Summarize(context.Background(), file)
		if err != nil || result.Created || result.SkippedReason == "" {
			t.Fatalf("%s: result = %+v, err = %v", file.Path, result, err)
		}
	}
	if len(client.requests) != 0 {
		t.Fatal("model was called for skipped files")
	}
}
