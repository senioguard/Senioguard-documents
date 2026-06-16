package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"senioguard-documents/internal/model"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Qdrant struct {
	BaseURL    string
	Collection string
	Client     *http.Client
}

func NewQdrant(host string) Qdrant {
	host = strings.TrimSpace(host)
	if strings.HasSuffix(host, ":6334") {
		host = strings.TrimSuffix(host, ":6334") + ":6333"
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return Qdrant{BaseURL: strings.TrimRight(host, "/"), Collection: "documents"}
}

func (q Qdrant) EnsureCollection(ctx context.Context, dimensions int) error {
	body := map[string]any{"vectors": map[string]any{"size": dimensions, "distance": "Cosine"}}
	return q.request(ctx, http.MethodPut, "/collections/"+q.Collection, body, nil)
}

func (q Qdrant) UpsertChunks(ctx context.Context, chunks []model.Chunk, vectors [][]float32) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("chunk/vector count mismatch")
	}
	points := make([]map[string]any, len(chunks))
	for i, chunk := range chunks {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(chunk.DocumentID.Hex()+fmt.Sprintf(":%d", chunk.ChunkIndex))).String()
		payload := map[string]any{
			"documentId":  chunk.DocumentID.Hex(),
			"displayName": chunk.DisplayName,
			"chunkIndex":  chunk.ChunkIndex,
			"text":        chunk.Text,
		}
		if chunk.CollectionID != nil {
			payload["collectionId"] = chunk.CollectionID.Hex()
		}
		points[i] = map[string]any{"id": id, "vector": vectors[i], "payload": payload}
	}
	return q.request(ctx, http.MethodPut, "/collections/"+q.Collection+"/points?wait=true", map[string]any{"points": points}, nil)
}

func (q Qdrant) Search(ctx context.Context, vector []float32, topK int, collectionID *primitive.ObjectID) ([]model.RAGSource, error) {
	body := map[string]any{"vector": vector, "limit": topK, "with_payload": true}
	if collectionID != nil {
		body["filter"] = map[string]any{"must": []map[string]any{{"key": "collectionId", "match": map[string]any{"value": collectionID.Hex()}}}}
	}
	var response struct {
		Result []struct {
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := q.request(ctx, http.MethodPost, "/collections/"+q.Collection+"/points/search", body, &response); err != nil {
		return nil, err
	}
	sources := make([]model.RAGSource, 0, len(response.Result))
	for _, hit := range response.Result {
		docID, _ := primitive.ObjectIDFromHex(asString(hit.Payload["documentId"]))
		sources = append(sources, model.RAGSource{
			DocumentID:  docID,
			DisplayName: asString(hit.Payload["displayName"]),
			ChunkIndex:  int(asFloat(hit.Payload["chunkIndex"])),
			Text:        asString(hit.Payload["text"]),
			Score:       hit.Score,
		})
	}
	return sources, nil
}

func (q Qdrant) DeleteDocument(ctx context.Context, documentID primitive.ObjectID) error {
	body := map[string]any{"filter": map[string]any{"must": []map[string]any{{"key": "documentId", "match": map[string]any{"value": documentID.Hex()}}}}}
	return q.request(ctx, http.MethodPost, "/collections/"+q.Collection+"/points/delete?wait=true", body, nil)
}

func (q Qdrant) request(ctx context.Context, method, path string, body any, dest any) error {
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := q.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("qdrant %s %s: %s", method, path, res.Status)
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	default:
		return 0
	}
}
