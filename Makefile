# Build configuration
BIN_DIR := bin
CMD_DIR := cmd/frkrcfg

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: ## Run tests.
	go test ./... -v -coverprofile cover.out

.PHONY: test-coverage
test-coverage: test ## Run tests with coverage report.
	go tool cover -html=cover.out -o cover.html
	@echo "Coverage report generated: cover.html"

##@ Build

.PHONY: build
build: fmt vet ## Build all binaries.
	@mkdir -p $(BIN_DIR)
	@for cmd in $$(find cmd -type d -mindepth 1 -maxdepth 1 | xargs -n1 basename); do \
		echo "Building $$cmd..."; \
		go build -o $(BIN_DIR)/$$cmd ./cmd/$$cmd; \
		echo "✅ Built $$cmd -> $(BIN_DIR)/$$cmd"; \
	done
	@echo ""
	@echo "All binaries built successfully in $(BIN_DIR)/"

.PHONY: clean
clean: ## Clean build artifacts.
	rm -rf $(BIN_DIR)
	rm -f cover.out cover.html

.PHONY: install
install: build ## Install binaries to GOPATH/bin.
	go install $(CMD_DIR)/main.go

##@ Verification

.PHONY: verify
verify: fmt vet test ## Run all verification checks.

