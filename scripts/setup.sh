#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SUDO=""
if [[ "${EUID}" -ne 0 ]]; then
  SUDO="sudo"
fi

log() {
  printf "\n==> %s\n" "$*"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

compose() {
  if $SUDO docker compose version >/dev/null 2>&1; then
    $SUDO docker compose "$@"
  elif have docker-compose; then
    $SUDO docker-compose "$@"
  else
    echo "Docker Compose is not installed." >&2
    exit 1
  fi
}

env_value() {
  local key="$1"
  if [[ ! -f .env ]]; then
    return
  fi
  grep -E "^${key}=" .env | tail -n 1 | cut -d= -f2-
}

should_enable_ollama() {
  [[ "${ENABLE_OLLAMA:-0}" == "1" ]] && return 0
  [[ "${PULL_OLLAMA_MODELS:-0}" == "1" ]] && return 0
  [[ "$(env_value AI_PROVIDER)" == "ollama" ]]
}

install_apt_packages() {
  if ! have apt-get; then
    echo "This setup script currently supports Debian/Ubuntu systems with apt-get." >&2
    echo "Install Go 1.21+, Docker, and Docker Compose manually, then run: docker compose up -d" >&2
    exit 1
  fi

  local packages=(
    ca-certificates
    curl
    git
    gnupg
    make
  )

  if ! have go || ! have gofmt; then
    packages+=(golang-go)
  fi

  if ! have docker; then
    packages+=(docker.io)
  fi

  if ! $SUDO docker compose version >/dev/null 2>&1 && ! have docker-compose; then
    packages+=(docker-compose-plugin)
  fi

  log "Installing missing system packages"
  $SUDO apt-get update
  $SUDO apt-get install -y "${packages[@]}"
}

ensure_docker_running() {
  log "Ensuring Docker is running"
  if command -v systemctl >/dev/null 2>&1; then
    $SUDO systemctl enable --now docker || true
  fi
  if ! $SUDO docker info >/dev/null 2>&1; then
    $SUDO service docker start || true
  fi
  if ! $SUDO docker info >/dev/null 2>&1; then
    echo "Docker is installed but not reachable. Try running this script with sudo or check the Docker daemon." >&2
    exit 1
  fi
}

ensure_env() {
  if [[ ! -f .env ]]; then
    log "Creating .env from .env.example"
    cp .env.example .env
    if have openssl; then
      local secret
      secret="$(openssl rand -hex 32)"
      sed -i "s/^SESSION_SECRET=.*/SESSION_SECRET=${secret}/" .env
    fi
  else
    log "Keeping existing .env"
  fi
}

download_go_modules() {
  if ! have go || ! have gofmt; then
    echo "Go or gofmt is still missing after package installation." >&2
    exit 1
  fi

  log "Downloading Go modules"
  go mod download
}

start_services() {
  if should_enable_ollama; then
    log "Starting MongoDB, Qdrant, MinIO, Ollama, and Caddy"
    compose --profile ollama up -d
  else
    log "Starting MongoDB, Qdrant, MinIO, and Caddy"
    compose up -d
  fi
}

maybe_pull_ollama_models() {
  if [[ "${PULL_OLLAMA_MODELS:-0}" != "1" ]]; then
    return
  fi

  log "Pulling Ollama models"
  compose --profile ollama up -d ollama
  local llm_model embed_model
  llm_model="$(grep '^OLLAMA_LLM_MODEL=' .env | cut -d= -f2-)"
  embed_model="$(grep '^OLLAMA_EMBED_MODEL=' .env | cut -d= -f2-)"
  compose exec -T ollama ollama pull "${llm_model:-llama3}"
  compose exec -T ollama ollama pull "${embed_model:-nomic-embed-text}"
}

main() {
  install_apt_packages
  ensure_docker_running
  ensure_env
  download_go_modules
  start_services
  maybe_pull_ollama_models

  log "Setup complete"
  cat <<'MSG'

Next steps:
  1. Edit .env and set NVIDIA_API_KEY, or set AI_PROVIDER=ollama.
  2. If using Ollama, optionally run:
       PULL_OLLAMA_MODELS=1 ./scripts/setup.sh
     To start Ollama without pulling models:
       ENABLE_OLLAMA=1 ./scripts/setup.sh
  3. Start the app:
       go run ./cmd/server

Useful URLs:
  App:           http://localhost:8080
  Caddy proxy:   https://localhost
  MinIO console: http://localhost:9001
  Qdrant:        http://localhost:6333/dashboard
MSG
}

main "$@"
