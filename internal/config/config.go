package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port              string
	MongoURI          string
	QdrantHost        string
	StorageModule     string
	LocalStoragePath  string
	MinIOEndpoint     string
	MinIOBucket       string
	MinIOAccessKey    string
	MinIOSecretKey    string
	MinIOUseSSL       bool
	AIProvider        string
	NVIDIAAPIKey      string
	NVIDIABaseURL     string
	NVIDIALLMModel    string
	NVIDIAEmbedModel  string
	OllamaHost        string
	OllamaLLMModel    string
	OllamaEmbedModel  string
	ProcessorWorkers  int
	AuthUser          string
	AuthPassword      string
	Domain            string
	SessionSecret     string
	RAGTopK           int
	GitHubEnabled     bool
	GitHubToken       string
	GitHubRepos       []string
	GitHubSyncOnStart bool
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		Port:              env("PORT", "8080"),
		MongoURI:          env("MONGO_URI", "mongodb://localhost:27017/docmanager"),
		QdrantHost:        env("QDRANT_HOST", "localhost:6334"),
		StorageModule:     strings.ToLower(env("STORAGE_MODULE", "local")),
		LocalStoragePath:  env("LOCAL_STORAGE_PATH", "./data/files"),
		MinIOEndpoint:     env("MINIO_ENDPOINT", "localhost:9000"),
		MinIOBucket:       env("MINIO_BUCKET", "docmanager"),
		MinIOAccessKey:    env("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:    env("MINIO_SECRET_KEY", "minioadmin"),
		MinIOUseSSL:       envBool("MINIO_USE_SSL", false),
		AIProvider:        strings.ToLower(env("AI_PROVIDER", "nvidia")),
		NVIDIAAPIKey:      os.Getenv("NVIDIA_API_KEY"),
		NVIDIABaseURL:     env("NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1"),
		NVIDIALLMModel:    env("NVIDIA_LLM_MODEL", "nvidia/llama-3.3-nemotron-super-49b-v1"),
		NVIDIAEmbedModel:  env("NVIDIA_EMBED_MODEL", "nvidia/nv-embedqa-e5-v5"),
		OllamaHost:        env("OLLAMA_HOST", "http://localhost:11434"),
		OllamaLLMModel:    env("OLLAMA_LLM_MODEL", "llama3"),
		OllamaEmbedModel:  env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		ProcessorWorkers:  envInt("PROCESSOR_WORKERS", 3),
		AuthUser:          env("AUTH_USER", "admin"),
		AuthPassword:      env("AUTH_PASSWORD", "changeme"),
		Domain:            env("DOMAIN", "localhost"),
		SessionSecret:     env("SESSION_SECRET", "change-this-secret"),
		RAGTopK:           envInt("RAG_TOP_K", 5),
		GitHubEnabled:     envBool("GITHUB_ENABLED", false),
		GitHubToken:       os.Getenv("GITHUB_TOKEN"),
		GitHubRepos:       envCSV("GITHUB_REPOS"),
		GitHubSyncOnStart: envBool("GITHUB_SYNC_ON_START", false),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(env(key, ""))
	switch value {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}

func envCSV(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
