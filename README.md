# Senioguard Document Management System

A single-binary Go document manager with session auth, MongoDB metadata, Qdrant vector search, local or MinIO storage, HTMX/Alpine/Tailwind UI, and RAG through NVIDIA NIM or Ollama.

## Run

Follow these steps to set up and start the application:

### 1. Run Docker Services
The application requires several backend services. You can start them in the background using Docker Compose:

```bash
docker compose up -d
```
This starts:
- **MongoDB** (`localhost:27017`) - Metadata store.
- **Qdrant** (`localhost:6333` / `localhost:6334`) - Vector database.
- **MinIO** (`localhost:9000` / `localhost:9001`) - Document storage server.
- **Caddy** (`localhost:80` / `localhost:443`) - Reverse proxy.

### 2. Set Up Environment Variables
Copy `.env.example` to `.env` and fill in your details:

```bash
cp .env.example .env
```

Make sure to set your preferred `AI_PROVIDER` (e.g., `deepinfra` or `nvidia`) and their corresponding API keys.

### 3. Build & Run the Go Server
Run the single-binary Go application backend server:

```bash
go run ./cmd/server
```

Or run via `make`:

```bash
make run
```

The Go server runs on port `8080`.

### Troubleshooting Background Services
If you see an error like `bind: address already in use` when starting the server, or want to verify if a background instance is already running:

1. **Check if port 8080 is already bound:**
   ```bash
   lsof -i :8080
   ```
2. **Stop the existing process:**
   ```bash
   kill <PID>
   ```

---

Fresh clone quickstart on Debian/Ubuntu:

```bash
./scripts/setup.sh
```

The setup script installs missing host packages, creates `.env`, downloads Go modules, and starts MongoDB, Qdrant, MinIO, and Caddy with Docker Compose.

Then edit `.env` and set the provider keys you need. By default the app uses NVIDIA-compatible chat and NVIDIA embeddings, but chat and embeddings can be selected independently:

```bash
AI_PROVIDER=deepinfra

EMBED_PROVIDER=deepinfra
DEEPINFRA_API_KEY=...
DEEPINFRA_LLM_MODEL=deepseek-ai/DeepSeek-V4-Flash
DEEPINFRA_EMBED_MODEL=BAAI/bge-m3
```

Or switch to local Ollama:

```bash
AI_PROVIDER=ollama
```

Ollama is optional and only starts when `AI_PROVIDER=ollama`, `ENABLE_OLLAMA=1`, or `PULL_OLLAMA_MODELS=1` is set. If using Ollama, pull the default local models:

```bash
PULL_OLLAMA_MODELS=1 ./scripts/setup.sh
```

Start the Go app:

```bash
go run ./cmd/server
```

Open `http://localhost:8080` and log in with `AUTH_USER` / `AUTH_PASSWORD`.

Useful service URLs:

- App: `http://localhost:8080`
- Caddy proxy: `https://localhost`
- MinIO console: `http://localhost:9001`
- Qdrant dashboard: `http://localhost:6333/dashboard`

Convenience commands:

```bash
make setup
make setup-ollama
make services-up
make services-up-ollama
make run
make test
make services-down
```

## Modules

Every capability is injected through `internal/module` interfaces:

- `Storage`: `STORAGE_MODULE=local|minio`
- `Extractor`: Markdown, plain text, DOCX, and high-quality PDF text extraction powered by `github.com/lightningrag/pdf-go` (providing robust ToUnicode and full font-map parsing to handle complex layout styles/multi-page PDFs correctly).
- `LLM`: `AI_PROVIDER=nvidia|deepinfra|ollama`
- `Embedder`: `EMBED_PROVIDER=nvidia|deepinfra|ollama`
- `Chunker`: word and sentence implementations
- `VectorDB`: Qdrant wrapper
- `SourceConnector`: GitHub repo, issue, PR, and release sync

The processor runs a buffered worker pool and performs `extract -> chunk -> embed -> qdrant upsert -> LLM labels`.

## GitHub Integration

Enable GitHub sync with:

```bash
GITHUB_ENABLED=true
GITHUB_TOKEN=ghp_or_fine_grained_token
GITHUB_REPOS=owner/repo,owner2/repo2
GITHUB_SYNC_ON_START=false
```

Then click **Sync GitHub** in the sidebar or call:

```bash
curl -X POST http://localhost:8080/api/sources/github/sync
```

The connector imports:

- Markdown/text documentation files from the default branch
- Issues
- Pull requests
- Releases

Each item is stored as a normal document under `GitHub/<owner>/<repo>/...`, with provenance metadata (`source`, `sourceType`, `externalUrl`, `repository`, `author`) and then queued through the same extract/chunk/embed/label pipeline as uploaded files.

## Notes

- Qdrant's standard REST port is `6333`; if `QDRANT_HOST` is set to `localhost:6334`, the wrapper maps it to `localhost:6333`.
- PDF extraction is powered by the `github.com/lightningrag/pdf-go` library. It extracts text accurately page-by-page by decoding stream objects, font encodings (WinAnsi, MacRoman, Custom CMaps), and ToUnicode maps. This enables the RAG pipeline to process complex, multi-page layout PDFs (such as pitch decks) beautifully.
- The UI uses CDN assets for HTMX, Alpine.js, Tailwind, Mermaid, Marked, and highlight.js.
