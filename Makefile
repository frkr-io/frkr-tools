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

.PHONY: test-e2e
test-e2e: ## Run E2E tests (requires kind, kubectl, helm, Docker).
	@echo "Running E2E tests for Kubernetes external access..."
	@echo "This will create a kind cluster and test LoadBalancer, Ingress, and ClusterIP configurations"
	go test -tags=e2e ./cmd/frkrup/... -v -run TestKubernetesExternalAccess

.PHONY: test-coverage
test-coverage: test ## Run tests with coverage report.
	go tool cover -html=cover.out -o cover.html
	@echo "Coverage report generated: cover.html"

##@ Build

.PHONY: build
build: fmt vet build-frkrcfg build-frkrup ## Build all binaries.

.PHONY: build-frkrcfg
build-frkrcfg: ## Build frkrcfg binary.
	@mkdir -p $(BIN_DIR)
	@echo "Building frkrcfg..."
	@go build -o $(BIN_DIR)/frkrcfg ./cmd/frkrcfg
	@echo "✅ Built frkrcfg -> $(BIN_DIR)/frkrcfg"

.PHONY: build-frkrup
build-frkrup: ## Build frkrup binary.
	@mkdir -p $(BIN_DIR)
	@echo "Building frkrup..."
	@go build -o $(BIN_DIR)/frkrup ./cmd/frkrup
	@echo "✅ Built frkrup -> $(BIN_DIR)/frkrup"
	@echo ""
	@echo "✅ All binaries built successfully in $(BIN_DIR)/"

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

