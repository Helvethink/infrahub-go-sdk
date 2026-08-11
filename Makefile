.PHONY: build check fmt fmt-check race test vet

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || true)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)" \
		-o bin/infrahubctl ./cmd/infrahubctl

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

check: fmt-check vet test
