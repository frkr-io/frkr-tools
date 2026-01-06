# frkr Orchestration Makefile

.PHONY: help build kind-up deploy verify-e2e clean

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
	cd frkr-ingest-gateway && go build -o bin/gateway ./cmd/gateway
	cd frkr-streaming-gateway && go build -o bin/gateway ./cmd/gateway
	cd frkr-operator && make build-operator

docker-build:
	cd frkr-ingest-gateway && docker build -t frkr-ingest-gateway:0.1.0 .
	cd frkr-streaming-gateway && docker build -t frkr-streaming-gateway:0.1.0 .
	cd frkr-operator && docker build -t frkr-operator:0.1.1 .

kind-up:
	kind delete cluster --name frkr-dev || true
	kind create cluster --name frkr-dev
	$(MAKE) load-images

load-images:
	kind load docker-image frkr-ingest-gateway:0.1.0 --name frkr-dev
	kind load docker-image frkr-streaming-gateway:0.1.0 --name frkr-dev
	kind load docker-image frkr-operator:0.1.1 --name frkr-dev

deploy:
	helm upgrade --install frkr frkr-infra-helm -f frkr-infra-helm/values-full.yaml

verify-e2e:
	@echo "Verifying E2E flow..."
	# Send test traffic
	curl -s -X POST http://localhost:8080/ingest \
		-H "Content-Type: application/json" \
		-d '{"stream_id": "my-api", "request": {"request_id": "e2e-test", "method": "GET", "path": "/verify"}}' \
		-u testuser:testpass
	@echo "Check example-api logs for [FORWARDED FROM FRKR]"

clean:
	helm delete frkr || true
	kind delete cluster --name frkr-dev || true
