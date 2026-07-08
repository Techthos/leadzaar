.PHONY: fmt lint test build tidy check run

fmt:
	gofumpt -w .

lint:
	golangci-lint run

test:
	go test ./... -race -cover

build:
	go build -o leadzaar .

tidy:
	go mod tidy

check: fmt tidy lint test

run:
	go run .
