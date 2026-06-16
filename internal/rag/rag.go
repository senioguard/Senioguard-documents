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

func (s Service) Query(ctx context.Context, question string, collectionID *primitive.ObjectID, documentIDs []primitive.ObjectID, topK int) (model.RAGResponse, error) {
	if strings.TrimSpace(question) == "" {
		return model.RAGResponse{}, fmt.Errorf("question is required")
	}
	if topK <= 0 {
		topK = s.topK
	}
	vectors, err := embedQuery(ctx, s.embedder, []string{question})
	if err != nil {
		return model.RAGResponse{}, err
	}
	sources, err := s.vectorDB.Search(ctx, vectors[0], topK, collectionID, documentIDs)
	if err != nil {
		return model.RAGResponse{}, err
	}
	var contextBuilder strings.Builder
	for i, source := range sources {
		fmt.Fprintf(&contextBuilder, "[%d] %s chunk %d\n%s\n\n", i+1, source.DisplayName, source.ChunkIndex, source.Text)
	}
	system := strings.Join([]string{
		"You answer questions from retrieved document chunks. Cite sources inline as [1], [2]. If context is insufficient, say so.",
		"When the user asks for a Mermaid diagram, put only valid Mermaid syntax inside a fenced ```mermaid block.",
		"Never put prose, citations, Markdown headings, or explanations inside the Mermaid fenced block. Put prose before or after the block.",
		"For Mermaid labels, prefer quoted labels and avoid raw parentheses or colons inside node IDs.",
	}, " ")
	user := fmt.Sprintf("Question: %s\n\nContext:\n%s", question, contextBuilder.String())
	answer, err := s.llm.Complete(ctx, system, user)
	if err != nil {
		return model.RAGResponse{}, err
	}
	return model.RAGResponse{Question: question, Answer: answer, Sources: sources}, nil
}

type queryEmbedder interface {
	EmbedQuery(ctx context.Context, texts []string) ([][]float32, error)
}

func embedQuery(ctx context.Context, embedder module.Embedder, texts []string) ([][]float32, error) {
	if qe, ok := embedder.(queryEmbedder); ok {
		return qe.EmbedQuery(ctx, texts)
	}
	return embedder.Embed(ctx, texts)
}
