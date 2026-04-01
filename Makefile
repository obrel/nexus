APP_NAME := nexus
VERSION  := $(shell git describe --tags --abbrev=0 2>/dev/null || git rev-parse --short HEAD)

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(APP_NAME)-$(VERSION) ./cmd

test:
	go test ./...

bench:
	go test -benchmem -run=^$$ -bench=. ./internal/...

cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
