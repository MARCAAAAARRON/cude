BINARY   = cude
MODULE   = github.com/marcar/cude
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS  = -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build install release clean test

## build: compile for the current platform
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/cude

## install: install into $GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/cude

## test: run all tests
test:
	go test ./...

## release: cross-compile for linux, darwin, windows
release: clean
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-amd64        ./cmd/cude
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-arm64        ./cmd/cude
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-amd64       ./cmd/cude
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-arm64       ./cmd/cude
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-windows-amd64.exe  ./cmd/cude

## clean: remove build artifacts
clean:
	rm -rf bin/
