package rag

import (
	"context"
	"fmt"
	"strings"

	"senioguard-documents/internal/model"
	"senioguard-documents/internal/module"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Service struct {
	embedder module.Embedder
	llm      module.LLM
	vectorDB module.VectorDB
	topK     int
}

func New(embedder module.Embedder, llm module.LLM, vectorDB module.VectorDB, topK int) Service {
	if topK <= 0 {
		topK = 5
	}
	return Service{embedder: embedder, llm: llm, vectorDB: vectorDB, topK: topK}
}

func (s Service) Query(ctx context.Context, question string, collectionID *primitive.ObjectID, topK int) (model.RAGResponse, error) {
	if strings.TrimSpace(question) == "" {
		return model.RAGResponse{}, fmt.Errorf("question is required")
	}
	if topK <= 0 {
		topK = s.topK
	}
	vectors, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return model.RAGResponse{}, err
	}
	sources, err := s.vectorDB.Search(ctx, vectors[0], topK, collectionID)
	if err != nil {
		return model.RAGResponse{}, err
	}
	var contextBuilder strings.Builder
	for i, source := range sources {
		fmt.Fprintf(&contextBuilder, "[%d] %s chunk %d\n%s\n\n", i+1, source.DisplayName, source.ChunkIndex, source.Text)
	}
	system := "You answer questions from retrieved document chunks. Cite sources inline as [1], [2]. If context is insufficient, say so."
	user := fmt.Sprintf("Question: %s\n\nContext:\n%s", question, contextBuilder.String())
	answer, err := s.llm.Complete(ctx, system, user)
	if err != nil {
		return model.RAGResponse{}, err
	}
	return model.RAGResponse{Answer: answer, Sources: sources}, nil
}
