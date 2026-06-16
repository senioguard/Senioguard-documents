.PHONY: setup setup-ollama services-up services-up-ollama services-down run test fmt logs

setup:
	./scripts/setup.sh

setup-ollama:
	ENABLE_OLLAMA=1 ./scripts/setup.sh

services-up:
	docker compose up -d

services-up-ollama:
	docker compose --profile ollama up -d

services-down:
	docker compose down

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w cmd internal

logs:
	docker compose logs -f
