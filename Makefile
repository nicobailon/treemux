APP := treemux
PKG := github.com/nicobailon/treemux

.PHONY: build build-all test lint release install tidy

build:
	go build ./cmd/treemux

build-all:
	GOOS=darwin GOARCH=amd64 go build -o dist/$(APP)_darwin_amd64 ./cmd/treemux
	GOOS=darwin GOARCH=arm64 go build -o dist/$(APP)_darwin_arm64 ./cmd/treemux
	GOOS=linux GOARCH=amd64 go build -o dist/$(APP)_linux_amd64 ./cmd/treemux
	GOOS=linux GOARCH=arm64 go build -o dist/$(APP)_linux_arm64 ./cmd/treemux

test:
	go test ./...

lint:
	golangci-lint run ./...

release: tidy test build-all

install:
	go install $(PKG)/cmd/treemux@latest

tidy:
	go mod tidy
