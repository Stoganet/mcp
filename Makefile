.PHONY: test lint build run tidy

GO ?= go

test:
	$(GO) test -race -count=1 ./...

lint:
	golangci-lint run

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o dist/mcp-server ./cmd/mcp-server

run:
	$(GO) run ./cmd/mcp-server

tidy:
	$(GO) mod tidy
