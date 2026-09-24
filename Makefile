BINARY  := terraform-provider-synclayer
VERSION ?= 0.1.0
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)
PLATFORM := $(GOOS)_$(GOARCH)

.PHONY: default build install test fmt vet docs clean release

default: build

build:
	go build -o $(BINARY) .

install: build
	mkdir -p "$$HOME/.terraform.d/plugins/registry.terraform.io/usabarashi/synclayer/$(VERSION)/$(PLATFORM)"
	mv $(BINARY) "$$HOME/.terraform.d/plugins/registry.terraform.io/usabarashi/synclayer/$(VERSION)/$(PLATFORM)/"

test:
	go test ./... -count=1

fmt:
	gofmt -w .
	terraform fmt -recursive ./examples || true

vet:
	go vet ./...

docs:
	@echo "Docs are hand-written under ./docs and examples under ./examples."

clean:
	rm -f $(BINARY)

release: build
	zip "$(BINARY)_v$(VERSION)_$(PLATFORM).zip" $(BINARY)
