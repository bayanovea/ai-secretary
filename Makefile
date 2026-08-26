# secretary — сборка, тесты, установка.
#
# The binary is a single package with no code generation and no build tags, so
# these three targets are the whole of it.

BINARY ?= secretary
PKG    := ./cmd/secretary

.PHONY: all build test install clean fmt vet

all: build

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./...

install:
	go install $(PKG)

fmt:
	gofmt -l -w .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
