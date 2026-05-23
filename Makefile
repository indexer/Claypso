# Build-time variables set via -ldflags
VERSION  ?= dev
COMMIT   ?= unknown
DATE     ?= unknown

LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test lint vet clean install

build:
	go build -ldflags "$(LDFLAGS)" -o calypso ./cmd/calypso

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/calypso

test:
	go test ./... -count=1 -timeout 60s

test-int:
	go test ./cmd/calypso/ -count=1 -v -timeout 60s

lint:
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	rm -f calypso
	go clean -testcache

all: lint vet test build
