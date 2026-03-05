MODULE  := github.com/syst3mctl/crashctl
BINARY  := crashctl
CMD     := ./cmd/crashctl

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.1")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(DATE)

.PHONY: build test lint dev docker clean

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

dev:
	go run $(CMD) serve

docker:
	docker build -t syst3mctl/crashctl:$(VERSION) -f deploy/Dockerfile .

clean:
	rm -f $(BINARY)
	go clean -testcache
