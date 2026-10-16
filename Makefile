IMG ?= ghcr.io/dbslone/hermes-operator:dev
GO_IMAGE ?= golang:1.26
DOCKER_GO = docker run --rm -v "$(PWD)":/src -w /src $(GO_IMAGE)

.PHONY: help build fmt test cover chart-lint chart-test plans-check e2e image check
help:    ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/'

build:   ## compile + vet inside Docker (no local Go needed)
	$(DOCKER_GO) sh -c 'go vet ./... && go build -o /dev/null ./cmd'
fmt:     ## gofmt
	$(DOCKER_GO) gofmt -w .
test:    ## unit tests with the race detector
	$(DOCKER_GO) go test -count=1 -race ./...
cover:   ## unit tests + fail if coverage < 90%
	$(DOCKER_GO) test/coverage.sh
chart-lint: ## helm lint + template
	helm lint charts/hermes-operator && helm template t charts/hermes-operator >/dev/null
chart-test: ## assertions over rendered chart output
	test/chart.sh
plans-check: ## plans/ naming + README links (BASE_REF=origin/main also requires a new plan)
	test/plans.sh
e2e:     ## full install on the cluster in $KUBECONFIG; needs IMG loaded in it
	IMG=$(IMG) test/e2e.sh
image:   ## build the operator image
	docker build -t $(IMG) .
check: build cover chart-lint chart-test plans-check ## everything that doesn't need a cluster
