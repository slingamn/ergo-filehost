.PHONY: all build clean install test

export CGO_ENABLED ?= 0

# Binary name
BINARY := filehost

# Version information (can be overridden)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY) .

# Clean build artifacts
clean:
	rm -f $(BINARY)

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) .

# Run tests
test:
	go test -v ./...

# Build for multiple platforms
release:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-darwin-arm64 .
	GOOS=freebsd GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-freebsd-amd64 .
