# frkr Orchestration Makefile

.PHONY: help build kind-up sync-migrations deploy verify-e2e clean

help:
	@echo "frkr Orchestration Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build all gateway and operator binaries"
	@echo "  docker-build  Build all Docker images"
	@echo "  kind-up       Create or restart the Kind cluster"
	@echo "  deploy        Deploy frkr to Kubernetes using Helm (full stack)"
	@echo "  verify-e2e    Run end-to-end verification"
	@echo "  clean         Delete current deployment and Kind cluster"

build:
	@mkdir -p bin
	cd frkr-ingest-gateway && GOWORK=off go build -o bin/gateway ./cmd/gateway
	cd frkr-streaming-gateway && GOWORK=off go build -o bin/gateway ./cmd/gateway
	cd frkr-operator && GOWORK=off $(MAKE) build-operator
	cd frkr-operator && GOWORK=off go build -o ../bin/frkrctl ./cmd/frkrctl
	go build -o bin/frkrup ./cmd/frkrup
	go build -o bin/frkrcfg ./cmd/frkrcfg

docker-build:
	cd frkr-ingest-gateway && docker build -t frkr-ingest-gateway:0.1.0 .
	cd frkr-streaming-gateway && docker build -t frkr-streaming-gateway:0.1.0 .
	cd frkr-operator && docker build -t frkr-operator:0.1.1 .

kind-up:
	kind delete cluster --name frkr-dev || true
	kind create cluster --name frkr-dev

load-images:
	kind load docker-image frkr-ingest-gateway:0.1.0 --name frkr-dev
	kind load docker-image frkr-streaming-gateway:0.1.0 --name frkr-dev
	kind load docker-image frkr-operator:0.1.1 --name frkr-dev
	docker pull busybox
	docker pull postgres:15-alpine
	docker pull docker.redpanda.com/redpandadata/redpanda:latest
	kind load docker-image busybox --name frkr-dev
	kind load docker-image postgres:15-alpine --name frkr-dev
	kind load docker-image docker.redpanda.com/redpandadata/redpanda:latest --name frkr-dev

	@echo "Resolving frkr-common path and syncing migrations..."
	@COMMON_PATH=$$(go list -m -f '{{.Dir}}' github.com/frkr-io/frkr-common 2>/dev/null) || \
		COMMON_PATH=$$(pwd)/../frkr-common; \
	if [ ! -d "$$COMMON_PATH/migrations" ]; then \
		echo "Error: migrations directory not found at $$COMMON_PATH/migrations"; \
		exit 1; \
	fi; \
	cd frkr-infra-helm && \
	mkdir -p migrations && \
	cp $$COMMON_PATH/migrations/*.up.sql migrations/ && \
	echo "✅ Synced migrations from $$COMMON_PATH/migrations to frkr-infra-helm/migrations"

deploy: sync-migrations
	helm upgrade --install frkr frkr-infra-helm -f frkr-infra-helm/values-full.yaml

verify-e2e:
	@echo "Verifying E2E flow..."
	# Send test traffic
	curl -s -X POST http://localhost:8080/ingest \
		-H "Content-Type: application/json" \
		-d '{"stream_id": "my-api", "request": {"request_id": "e2e-test", "method": "GET", "path": "/verify"}}' \
		-u testuser:testpass
	@echo "Check example-api logs for [FORWARDED FROM FRKR]"

# Clean the bin folder
clean:
	rm -rf bin

kind-down:
	helm delete frkr || true
	kind delete cluster --name frkr-dev || true
