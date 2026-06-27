.PHONY: fmt-check vet test race build install install-toolchain

GO_PACKAGES := ./...
GO_FILES := $(shell find . -name '*.go' -not -path './.git/*' -not -path './.artifacts/*')
PREFIX ?= $(HOME)/.local
BINDIR := $(DESTDIR)$(PREFIX)/bin

fmt-check:
	@test -z "$$(gofmt -l $(GO_FILES))"

vet:
	go vet $(GO_PACKAGES)

test: fmt-check vet
	go test $(GO_PACKAGES)

race:
	go test -race $(GO_PACKAGES)

build:
	mkdir -p .artifacts/bin
	go build -o .artifacts/bin/thelper ./cmd/thelper
	go build -o .artifacts/bin/thelper-worker ./cmd/thelper-worker
	go build -o .artifacts/bin/thelper-ctl ./cmd/thelper-ctl

install-toolchain:
	mkdir -p "$(BINDIR)"
	go run ./cmd/thelper-toolchain -dir "$(BINDIR)"

install: build install-toolchain
	mkdir -p "$(BINDIR)"
	install -m 0755 .artifacts/bin/thelper "$(BINDIR)/thelper"
	install -m 0755 .artifacts/bin/thelper-worker "$(BINDIR)/thelper-worker"
	install -m 0755 .artifacts/bin/thelper-ctl "$(BINDIR)/thelper-ctl"
