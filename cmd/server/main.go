package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"senioguard-documents/internal/api"
	"senioguard-documents/internal/chunker"
	"senioguard-documents/internal/config"
	"senioguard-documents/internal/db"
	"senioguard-documents/internal/embedder"
	"senioguard-documents/internal/extractor"
	"senioguard-documents/internal/llm"
	"senioguard-documents/internal/module"
	"senioguard-documents/internal/processor"
	"senioguard-documents/internal/rag"
	"senioguard-documents/internal/repository"
	"senioguard-documents/internal/source"
	"senioguard-documents/internal/storage"
	"senioguard-documents/internal/ui"
	"senioguard-documents/internal/vectordb"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mongoDB, err := db.Connect(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("connect mongo: %v", err)
	}
	repos := repository.New(mongoDB)
	if err := repos.EnsureIndexes(ctx); err != nil {
		log.Fatalf("ensure indexes: %v", err)
	}

	storageModule, err := buildStorage(ctx, cfg)
	if err != nil {
		log.Fatalf("build storage: %v", err)
	}
	llmModule, embedderModule, err := buildAI(cfg)
	if err != nil {
		log.Fatalf("build ai: %v", err)
	}
	vectorDB := vectordb.NewQdrant(cfg.QdrantHost)
	registry := extractor.NewRegistry(
		extractor.MarkdownExtractor{},
		extractor.PlainTextExtractor{},
		extractor.PDFExtractor{},
		extractor.DOCXExtractor{},
	)
	chunkerModule := chunker.NewWord(1024, 200)
	processorModule := processor.New(repos, storageModule, registry, chunkerModule, embedderModule, llmModule, vectorDB, cfg.ProcessorWorkers)
	ragService := rag.New(embedderModule, llmModule, vectorDB, cfg.RAGTopK)
	sourceConnectors := buildSources(cfg, repos, storageModule, processorModule)
	if cfg.GitHubSyncOnStart {
		for _, connector := range sourceConnectors {
			go func(connector module.SourceConnector) {
				result, err := connector.Sync(context.Background())
				if err != nil {
					log.Printf("source sync %s failed: %v", connector.Name(), err)
					return
				}
				log.Printf("source sync %s: created=%d updated=%d skipped=%d queued=%d", result.Source, result.Created, result.Updated, result.Skipped, result.Processed)
			}(connector)
		}
	}
	templates, err := ui.Templates()
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      api.New(cfg, repos, storageModule, processorModule, ragService, vectorDB, sourceConnectors, templates),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("document manager listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildSources(cfg config.Config, repos *repository.Repositories, storageModule module.Storage, processorModule *processor.Processor) []module.SourceConnector {
	var connectors []module.SourceConnector
	if cfg.GitHubEnabled && len(cfg.GitHubRepos) > 0 {
		connectors = append(connectors, &source.GitHubConnector{
			Token:   cfg.GitHubToken,
			Repos:   cfg.GitHubRepos,
			Storage: storageModule,
			ReposDB: repos,
			Enqueue: processorModule.Enqueue,
		})
	}
	return connectors
}

func buildStorage(ctx context.Context, cfg config.Config) (module.Storage, error) {
	switch cfg.StorageModule {
	case "local":
		return storage.NewLocal(cfg.LocalStoragePath), nil
	case "minio":
		s, err := storage.NewMinIO(cfg.MinIOEndpoint, cfg.MinIOBucket, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseSSL)
		if err != nil {
			return nil, err
		}
		return s, s.EnsureBucket(ctx)
	default:
		return nil, fmt.Errorf("unsupported STORAGE_MODULE %q", cfg.StorageModule)
	}
}

func buildAI(cfg config.Config) (module.LLM, module.Embedder, error) {
	llmModule, err := buildLLM(cfg)
	if err != nil {
		return nil, nil, err
	}
	embedderModule, err := buildEmbedder(cfg)
	if err != nil {
		return nil, nil, err
	}
	return llmModule, embedderModule, nil
}

func buildLLM(cfg config.Config) (module.LLM, error) {
	switch cfg.AIProvider {
	case "nvidia":
		return llm.NIMClient{BaseURL: cfg.NVIDIABaseURL, APIKey: cfg.NVIDIAAPIKey, Model: cfg.NVIDIALLMModel}, nil
	case "deepinfra":
		return llm.DeepInfraLLM{BaseURL: cfg.DeepInfraBaseURL, APIKey: cfg.DeepInfraAPIKey, Model: cfg.DeepInfraLLMModel}, nil
	case "ollama":
		return llm.OllamaLLM{Host: cfg.OllamaHost, Model: cfg.OllamaLLMModel}, nil
	default:
		return nil, fmt.Errorf("unsupported AI_PROVIDER %q", cfg.AIProvider)
	}
}

func buildEmbedder(cfg config.Config) (module.Embedder, error) {
	switch cfg.EmbedProvider {
	case "nvidia":
		return embedder.NIMEmbedder{BaseURL: cfg.NVIDIABaseURL, APIKey: cfg.NVIDIAAPIKey, Model: cfg.NVIDIAEmbedModel}, nil
	case "deepinfra":
		return embedder.DeepInfraEmbedder{BaseURL: cfg.DeepInfraBaseURL, APIKey: cfg.DeepInfraAPIKey, Model: cfg.DeepInfraModel, DimensionsN: cfg.DeepInfraDim}, nil
	case "ollama":
		return embedder.OllamaEmbedder{Host: cfg.OllamaHost, Model: cfg.OllamaEmbedModel}, nil
	default:
		return nil, fmt.Errorf("unsupported EMBED_PROVIDER %q", cfg.EmbedProvider)
	}
}
