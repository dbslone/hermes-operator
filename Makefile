IMG ?= ghcr.io/dbslone/hermes-operator:dev
GO_IMAGE ?= golang:1.26

.PHONY: build fmt chart-lint image
build:   ## compile + vet inside Docker (no local Go needed)
	docker run --rm -v "$(PWD)":/src -w /src $(GO_IMAGE) sh -c 'go vet ./... && go build -o /dev/null ./cmd'
fmt:
	docker run --rm -v "$(PWD)":/src -w /src $(GO_IMAGE) gofmt -w .
chart-lint:
	helm lint charts/hermes-operator && helm template t charts/hermes-operator >/dev/null
image:
	docker build -t $(IMG) .
