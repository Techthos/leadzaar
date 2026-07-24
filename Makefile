.PHONY: fmt lint test build tidy check run inspect

GO := go

fmt:
	gofumpt -w .

lint:
	golangci-lint run

test:
	go test ./... -race -cover

build:
	CGO_ENABLED=0 $(GO) build -o bin/leadzaar .

tidy:
	go mod tidy

check: fmt tidy lint test

run:
	go run .

inspect: build
	npx @modelcontextprotocol/inspector ./bin/leadzaar -mode mcp
