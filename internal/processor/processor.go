package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"senioguard-documents/internal/extractor"
	"senioguard-documents/internal/model"
	"senioguard-documents/internal/module"
	"senioguard-documents/internal/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Processor struct {
	repos     *repository.Repositories
	storage   module.Storage
	extractor extractor.Registry
	chunker   module.Chunker
	embedder  module.Embedder
	llm       module.LLM
	vectorDB  module.VectorDB
	jobs      chan primitive.ObjectID
}

func New(repos *repository.Repositories, storage module.Storage, extractor extractor.Registry, chunker module.Chunker, embedder module.Embedder, llm module.LLM, vectorDB module.VectorDB, workers int) *Processor {
	if workers <= 0 {
		workers = 1
	}
	p := &Processor{
		repos: repos, storage: storage, extractor: extractor, chunker: chunker,
		embedder: embedder, llm: llm, vectorDB: vectorDB,
		jobs: make(chan primitive.ObjectID, workers*4),
	}
	var once sync.Once
	once.Do(func() {
		for i := 0; i < workers; i++ {
			go p.worker()
		}
	})
	return p
}

func (p *Processor) Enqueue(id primitive.ObjectID) {
	select {
	case p.jobs <- id:
	default:
		go func() { p.jobs <- id }()
	}
}

func (p *Processor) worker() {
	for id := range p.jobs {
		if err := p.Process(context.Background(), id); err != nil {
			log.Printf("process document %s: %v", id.Hex(), err)
		}
	}
}

func (p *Processor) Process(ctx context.Context, id primitive.ObjectID) error {
	doc, err := p.repos.Documents.Get(ctx, id)
	if err != nil {
		return err
	}
	_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusProcessing, "")
	rc, err := p.storage.Download(ctx, doc.StorageKey)
	if err != nil {
		_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusError, err.Error())
		return err
	}
	defer rc.Close()
	text, err := p.extractor.Extract(doc.MIME, rc)
	if err != nil {
		_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusError, err.Error())
		return err
	}
	chunkTexts := p.chunker.Chunk(text)
	if len(chunkTexts) == 0 && strings.TrimSpace(text) != "" {
		chunkTexts = []string{text}
	}
	vectors, err := p.embedder.Embed(ctx, chunkTexts)
	if err != nil {
		_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusError, err.Error())
		return err
	}
	if err := p.vectorDB.EnsureCollection(ctx, p.embedder.Dimensions()); err != nil {
		_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusError, err.Error())
		return err
	}
	chunks := make([]model.Chunk, len(chunkTexts))
	for i, chunk := range chunkTexts {
		chunks[i] = model.Chunk{DocumentID: doc.ID, CollectionID: doc.CollectionID, DisplayName: doc.DisplayName, ChunkIndex: i, Text: chunk}
	}
	if err := p.vectorDB.DeleteDocument(ctx, doc.ID); err != nil {
		log.Printf("delete old vectors for %s: %v", doc.ID.Hex(), err)
	}
	if err := p.vectorDB.UpsertChunks(ctx, chunks, vectors); err != nil {
		_ = p.repos.Documents.UpdateStatus(ctx, id, model.StatusError, err.Error())
		return err
	}
	labels, category, summary := p.label(ctx, doc.DisplayName, text)
	return p.repos.Documents.UpdateProcessed(ctx, id, text, summary, category, labels)
}

func (p *Processor) label(ctx context.Context, name, text string) ([]string, string, string) {
	system := "You label document management records. Return only compact JSON with keys labels (array of strings), category (string), summary (string)."
	excerpt := text
	if len(excerpt) > 6000 {
		excerpt = excerpt[:6000]
	}
	answer, err := p.llm.Complete(ctx, system, fmt.Sprintf("Document name: %s\n\nContent:\n%s", name, excerpt))
	if err != nil {
		return nil, "", ""
	}
	var parsed struct {
		Labels   []string `json:"labels"`
		Category string   `json:"category"`
		Summary  string   `json:"summary"`
	}
	answer = strings.Trim(answer, " \n\t`")
	answer = strings.TrimPrefix(answer, "json")
	answer = extractJSONObject(answer)
	if err := json.Unmarshal([]byte(answer), &parsed); err != nil {
		return nil, "", answer
	}
	return parsed.Labels, parsed.Category, parsed.Summary
}

func extractJSONObject(value string) string {
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start == -1 || end == -1 || end <= start {
		return value
	}
	return value[start : end+1]
}
