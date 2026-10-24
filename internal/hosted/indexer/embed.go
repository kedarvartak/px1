package indexer

import (
	"context"
	"errors"
	"fmt"
	"math"
)

type EmbeddingChunk struct {
	ID      int64
	Content string
}

type EmbeddingProvider interface {
	Embed(context.Context, string, []string) ([][]float32, error)
}

type StoredEmbedding struct {
	InstallationID int64
	RepositoryID   int64
	ChunkID        int64
	Model          string
	ModelVersion   string
	Dimensions     int
	Vector         []float32
}

type EmbeddingStore interface {
	UpsertEmbeddings(context.Context, []StoredEmbedding) error
}

type EmbeddingIndexer struct {
	Provider     EmbeddingProvider
	Store        EmbeddingStore
	Model        string
	ModelVersion string
	BatchSize    int
	MaxChunks    int
	Dimensions   int
}

func (i EmbeddingIndexer) Index(ctx context.Context, installationID, repositoryID int64, chunks []EmbeddingChunk) error {
	if installationID <= 0 || repositoryID <= 0 {
		return errors.New("installation and repository IDs must be positive")
	}
	if i.Provider == nil || i.Store == nil || i.Model == "" || i.ModelVersion == "" {
		return errors.New("embedding provider, store, model, and model version are required")
	}
	batchSize := i.BatchSize
	if batchSize <= 0 {
		batchSize = 64
	}
	maxChunks := i.MaxChunks
	if maxChunks <= 0 {
		maxChunks = 10_000
	}
	if len(chunks) > maxChunks {
		return fmt.Errorf("embedding request has %d chunks; limit is %d", len(chunks), maxChunks)
	}
	for start := 0; start < len(chunks); start += batchSize {
		end := min(start+batchSize, len(chunks))
		inputs := make([]string, end-start)
		for offset, chunk := range chunks[start:end] {
			if chunk.ID <= 0 || chunk.Content == "" {
				return fmt.Errorf("chunk at offset %d has no ID or content", start+offset)
			}
			inputs[offset] = chunk.Content
		}
		vectors, err := i.Provider.Embed(ctx, i.Model, inputs)
		if err != nil {
			return fmt.Errorf("embed batch starting at %d: %w", start, err)
		}
		if len(vectors) != len(inputs) {
			return fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vectors), len(inputs))
		}
		records := make([]StoredEmbedding, len(vectors))
		for offset, vector := range vectors {
			if err := validateVector(vector, i.Dimensions); err != nil {
				return fmt.Errorf("embedding for chunk %d: %w", chunks[start+offset].ID, err)
			}
			records[offset] = StoredEmbedding{
				InstallationID: installationID, RepositoryID: repositoryID,
				ChunkID: chunks[start+offset].ID, Model: i.Model, ModelVersion: i.ModelVersion,
				Dimensions: len(vector), Vector: append([]float32(nil), vector...),
			}
		}
		if err := i.Store.UpsertEmbeddings(ctx, records); err != nil {
			return fmt.Errorf("store embedding batch starting at %d: %w", start, err)
		}
	}
	return nil
}

func validateVector(vector []float32, dimensions int) error {
	if len(vector) == 0 {
		return errors.New("vector is empty")
	}
	if dimensions > 0 && len(vector) != dimensions {
		return fmt.Errorf("vector has %d dimensions; want %d", len(vector), dimensions)
	}
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("vector contains a non-finite value")
		}
	}
	return nil
}
