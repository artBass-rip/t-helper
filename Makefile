.PHONY: fmt-check vet test race

GO_PACKAGES := ./...
GO_FILES := $(shell find . -name '*.go' -not -path './.git/*' -not -path './.artifacts/*')

fmt-check:
	@test -z "$$(gofmt -l $(GO_FILES))"

vet:
	go vet $(GO_PACKAGES)

test: fmt-check vet
	go test $(GO_PACKAGES)

race:
	go test -race $(GO_PACKAGES)
