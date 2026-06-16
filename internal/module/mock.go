package module

import (
	"bytes"
	"context"
	"io"
	"strings"

	"senioguard-documents/internal/model"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MockLLM struct{}

func (MockLLM) Complete(ctx context.Context, system, user string) (string, error) {
	return "Mock response for: " + strings.TrimSpace(user), nil
}

type MockEmbedder struct {
	Dim int
}

func (m MockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	dim := m.Dim
	if dim == 0 {
		dim = 8
	}
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector := make([]float32, dim)
		for j, r := range text {
			vector[j%dim] += float32(r%31) / 31
		}
		vectors[i] = vector
	}
	return vectors, nil
}

func (m MockEmbedder) Dimensions() int {
	if m.Dim == 0 {
		return 8
	}
	return m.Dim
}

type MockExtractor struct {
	MIMEs []string
}

func (m MockExtractor) Extract(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	return string(data), err
}

func (m MockExtractor) SupportedMIMEs() []string {
	if len(m.MIMEs) == 0 {
		return []string{"text/plain"}
	}
	return m.MIMEs
}

type MockStorage struct {
	Files map[string][]byte
}

func (m *MockStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if m.Files == nil {
		m.Files = map[string][]byte{}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.Files[key] = data
	return nil
}

func (m *MockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.Files[key])), nil
}

func (m *MockStorage) Delete(ctx context.Context, key string) error {
	delete(m.Files, key)
	return nil
}

type MockChunker struct{}

func (MockChunker) Chunk(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []string{text}
}

type MockVectorDB struct {
	Sources []model.RAGSource
}

func (m *MockVectorDB) EnsureCollection(ctx context.Context, dimensions int) error {
	return nil
}

func (m *MockVectorDB) UpsertChunks(ctx context.Context, chunks []model.Chunk, vectors [][]float32) error {
	m.Sources = make([]model.RAGSource, len(chunks))
	for i, chunk := range chunks {
		m.Sources[i] = model.RAGSource{DocumentID: chunk.DocumentID, DisplayName: chunk.DisplayName, ChunkIndex: chunk.ChunkIndex, Text: chunk.Text}
	}
	return nil
}

func (m *MockVectorDB) Search(ctx context.Context, vector []float32, topK int, collectionID *primitive.ObjectID, documentIDs []primitive.ObjectID) ([]model.RAGSource, error) {
	if topK > 0 && topK < len(m.Sources) {
		return m.Sources[:topK], nil
	}
	return m.Sources, nil
}

func (m *MockVectorDB) DeleteDocument(ctx context.Context, documentID primitive.ObjectID) error {
	return nil
}
