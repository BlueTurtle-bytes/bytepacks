BINARY      := apexpacks
BUILD_FLAGS := -ldflags "-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"
GO          := go

.PHONY: build test lint clean install

build:
	$(GO) build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/apexpacks

test:
	$(GO) test ./...

lint:
	golangci-lint run ./...

install:
	$(GO) install $(BUILD_FLAGS) ./cmd/apexpacks

clean:
	rm -f bin/$(BINARY)
