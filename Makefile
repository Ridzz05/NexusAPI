.PHONY: fmt generate vet test build verify

fmt:
	gofmt -w cmd internal openapi

generate:
	sqlc generate

vet:
	go vet ./...

test:
	go test ./...

build:
	go build -o nexus-api ./cmd/api

verify: fmt vet test build
