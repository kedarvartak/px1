package indexer

import (
	"context"
	"testing"
)

type embeddingStub struct {
	batches [][]string
}

func (e *embeddingStub) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	e.batches = append(e.batches, append([]string(nil), inputs...))
	vectors := make([][]float32, len(inputs))
	for index := range inputs {
		vectors[index] = []float32{float32(index), 1, 2}
	}
	return vectors, nil
}

type embeddingStoreStub struct{ batches [][]StoredEmbedding }

func (s *embeddingStoreStub) UpsertEmbeddings(_ context.Context, records []StoredEmbedding) error {
	copyRecords := append([]StoredEmbedding(nil), records...)
	s.batches = append(s.batches, copyRecords)
	return nil
}

func TestEmbeddingIndexerBatchesAndPersistsVersion(t *testing.T) {
	provider := &embeddingStub{}
	store := &embeddingStoreStub{}
	indexer := EmbeddingIndexer{
		Provider: provider, Store: store, Model: "embed-small", ModelVersion: "2026-10-01",
		BatchSize: 2, Dimensions: 3,
	}
	chunks := []EmbeddingChunk{{1, "one"}, {2, "two"}, {3, "three"}, {4, "four"}, {5, "five"}}
	if err := indexer.Index(context.Background(), 7, 9, chunks); err != nil {
		t.Fatal(err)
	}
	if len(provider.batches) != 3 || len(provider.batches[0]) != 2 || len(provider.batches[2]) != 1 {
		t.Fatalf("provider batches = %#v", provider.batches)
	}
	if len(store.batches) != 3 || store.batches[0][0].ModelVersion != "2026-10-01" || store.batches[0][0].InstallationID != 7 {
		t.Fatalf("stored batches = %#v", store.batches)
	}
}

func TestEmbeddingIndexerRejectsMalformedProviderResponse(t *testing.T) {
	provider := malformedEmbeddingStub{}
	indexer := EmbeddingIndexer{Provider: provider, Store: &embeddingStoreStub{}, Model: "model", ModelVersion: "v1", Dimensions: 3}
	if err := indexer.Index(context.Background(), 1, 2, []EmbeddingChunk{{1, "one"}}); err == nil {
		t.Fatal("expected malformed vector to fail")
	}
}

type malformedEmbeddingStub struct{}

func (malformedEmbeddingStub) Embed(context.Context, string, []string) ([][]float32, error) {
	return [][]float32{{1, 2}}, nil
}

func TestEmbeddingIndexerCapsWorkBeforeCallingProvider(t *testing.T) {
	provider := &embeddingStub{}
	indexer := EmbeddingIndexer{Provider: provider, Store: &embeddingStoreStub{}, Model: "model", ModelVersion: "v1", MaxChunks: 1}
	if err := indexer.Index(context.Background(), 1, 2, []EmbeddingChunk{{1, "one"}, {2, "two"}}); err == nil {
		t.Fatal("expected chunk limit error")
	}
	if len(provider.batches) != 0 {
		t.Fatal("provider was called before limit validation")
	}
}
