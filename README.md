# Senioguard Document Management System

A single-binary Go document manager with session auth, MongoDB metadata, Qdrant vector search, local or MinIO storage, HTMX/Alpine/Tailwind UI, and RAG through NVIDIA NIM or Ollama.

## Run

Fresh clone on Debian/Ubuntu:

```bash
./scripts/setup.sh
```

The setup script installs missing host packages, creates `.env`, downloads Go modules, and starts MongoDB, Qdrant, MinIO, and Caddy with Docker Compose.

Then edit `.env` and set `NVIDIA_API_KEY`, or switch to local Ollama:

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
- `Extractor`: Markdown, plain text, PDF, DOCX registry by MIME type
- `LLM`: `AI_PROVIDER=nvidia|ollama`
- `Embedder`: `AI_PROVIDER=nvidia|ollama`
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
- PDF extraction is intentionally a simple built-in extractor. For production-grade PDFs and OCR, add another `Extractor` implementation and register it without changing the pipeline.
- The UI uses CDN assets for HTMX, Alpine.js, Tailwind, Mermaid, Marked, and highlight.js.
