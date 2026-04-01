APP_NAME := nexus
VERSION  := $(shell git describe --tags --abbrev=0 2>/dev/null || git rev-parse --short HEAD)
OS       := $(shell uname -s | tr A-Z a-z)
ARCH     := $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

release:
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags="-s -w" -o $(APP_NAME)-$(VERSION)-$(OS)-$(ARCH) ./cmd

test:
	go test ./...

bench:
	go test -benchmem -run=^$$ -bench=. ./internal/...

cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
