SHELL := /bin/bash
TARGETS := docli
PKGNAME := doclingclient
VERSION := 0.1.1
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X main.Version=$(VERSION)
GOLDFLAGS += -X main.Buildtime=$(BUILDTIME)
GOLDFLAGS += -w -s
GOFLAGS = -ldflags "$(GOLDFLAGS)"

GO_FILES := $(shell find . -name "*.go" -type f)

.PHONY: all
all: $(TARGETS)

$(TARGETS): %: cmd/%/main.go $(GO_FILES)
	go build -o $@ -ldflags "$(GOLDFLAGS)" ./cmd/$*

.PHONY: test
test:
	go test -v ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -f $(TARGETS)

.PHONY: update-all-deps
update-all-deps:
	go get -u -v ./... && go mod tidy
