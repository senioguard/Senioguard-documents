.PHONY: setup services-up services-down run test fmt logs

setup:
	./scripts/setup.sh

services-up:
	docker compose up -d

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
