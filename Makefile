.PHONY: build test lint fmt

build:
	go build -o bin/tidemark ./cmd/tidemark

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt
