# vibsl — queue-based Docker build & Kubernetes deploy API.
.DEFAULT_GOAL := help
GO        ?= go
BIN_DIR   ?= bin
CLUSTER   ?= vibsl

.PHONY: help
help: ## Show this help.
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the server and render binaries into ./bin.
	$(GO) build -o $(BIN_DIR)/server ./cmd/server
	$(GO) build -o $(BIN_DIR)/render ./cmd/render

.PHONY: test
test: ## Run unit tests.
	$(GO) test ./...

.PHONY: vet
vet: ## Run go vet.
	$(GO) vet ./...

.PHONY: run
run: ## Run the API server in the foreground.
	$(GO) run ./cmd/server

.PHONY: render
render: ## Render manifests from examples/job.json to stdout.
	$(GO) run ./cmd/render examples/job.json

.PHONY: cluster-up
cluster-up: ## Create a kind cluster + local registry (localhost:5001).
	./scripts/kind-with-registry.sh

.PHONY: cluster-down
cluster-down: ## Delete the kind cluster and registry container.
	kind delete cluster --name $(CLUSTER) || true
	docker rm -f kind-registry || true

.PHONY: submit
submit: ## POST examples/job.json to a running server.
	curl -sS -X POST localhost:8080/jobs \
		-H 'Content-Type: application/json' \
		--data @examples/job.json | tee /tmp/vibsl-submit.json

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf $(BIN_DIR)
