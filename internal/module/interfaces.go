package module

import (
	"context"
	"io"

	"senioguard-documents/internal/model"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Storage interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type Extractor interface {
	Extract(r io.Reader) (string, error)
	SupportedMIMEs() []string
}

type LLM interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

type Chunker interface {
	Chunk(text string) []string
}

type VectorDB interface {
	EnsureCollection(ctx context.Context, dimensions int) error
	UpsertChunks(ctx context.Context, chunks []model.Chunk, vectors [][]float32) error
	Search(ctx context.Context, vector []float32, topK int, collectionID *primitive.ObjectID) ([]model.RAGSource, error)
	DeleteDocument(ctx context.Context, documentID primitive.ObjectID) error
}

type SourceSyncResult struct {
	Source    string `json:"source"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Skipped   int    `json:"skipped"`
	Processed int    `json:"processed"`
}

type SourceConnector interface {
	Name() string
	Sync(ctx context.Context) (SourceSyncResult, error)
}
