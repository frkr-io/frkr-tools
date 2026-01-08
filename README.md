# frkr-tools

Local development tools for frkr.

## Purpose

frkr-tools provides command-line tools for managing frkr:
- **frkrcfg**: Direct database and configuration access (bypasses operator, works with any PostgreSQL-compatible database)
- **frkrup**: Interactive setup tool for both local Docker Compose and Kubernetes deployments

**Note**: For Kubernetes deployments using the operator, see [`frkr-operator`](https://github.com/frkr-io/frkr-operator) which includes `frkrctl` - a CLI tool for managing Kubernetes resources via CRDs (FrkrUser, FrkrStream, etc.).

## Tools

- **frkrcfg**: Direct configuration tool (streams, tenants, users)
- **frkrup**: Interactive setup tool for local Docker Compose or Kubernetes deployment
- **migrate**: Database migration tool (integrated into frkrcfg)

## Building

### Prerequisites

- Go 1.24+ (required for dependencies)
- Docker (required for running tests with testcontainers)

### Build Steps

#### Using Makefile (Recommended)

```bash
# Build all binaries to bin/
make build

# Format code
make fmt

# Run linter
make vet

# Run all verification checks (fmt + vet + test)
make verify

# Clean build artifacts
make clean

# Install to GOPATH/bin
make install
```

#### Alternative Build Methods

```bash
# Using build script
./build.sh

# Or using Go build script
go run build.go

# Build a specific binary manually
go build -o bin/frkrcfg ./cmd/frkrcfg
```

**Note**: All binaries are built to the `bin/` directory.

## Installation

### From Source

```bash
# Clone the repository with submodules
git clone --recurse-submodules https://github.com/frkr-io/frkr-tools.git
cd frkr-tools

# If you already cloned without submodules, initialize them:
git submodule update --init --recursive

# Build all binaries
make build
```

**Note**: `frkr-tools` uses git submodules for:
- `frkr-ingest-gateway` and `frkr-streaming-gateway` (required by `frkrup` for local gateway startup)
- `frkr-infra-helm` (required by `frkrup` for Kubernetes deployment)
- `frkr-infra-docker` (optional, enables `frkrup` to automatically start Docker Compose services)

**For Local Development**: If you're developing `frkr-tools` alongside `frkr-common` in a sibling directory structure (e.g., both in `~/git/frkr-io/`), you can uncomment the `replace` directive in `go.mod` to use the local version:

```bash
# Uncomment this line in go.mod:
# replace github.com/frkr-io/frkr-common => ../frkr-common
```

### Using go install

```bash
go install github.com/frkr-io/frkr-tools/cmd/frkrcfg@latest
```

## Usage

### frkrup - Interactive Setup Tool

`frkrup` is the easiest way to get frkr running. It supports both local Docker Compose and Kubernetes deployments.

**Local Docker Compose Setup:**
```bash
./bin/frkrup
# When prompted "Deploy to Kubernetes? (yes/no) [no]": press Enter or type "no"
```

**Kubernetes Setup:**
```bash
./bin/frkrup
# When prompted "Deploy to Kubernetes? (yes/no) [no]": type "yes"
# When prompted "Use port forwarding for local access? (yes/no) [yes]":
#   - Type "yes" for local development (kind cluster)
#   - Type "no" for production (managed cluster with LoadBalancer/Ingress)
```

For detailed guides, see:
- [Quick Start Guide](QUICKSTART.md) - Local Docker Compose setup
- [Kubernetes Quick Start Guide](K8S-QUICKSTART.md) - Kubernetes deployment

### frkrcfg - Direct Configuration

Use `frkrcfg` for direct database operations without going through the setup wizard:

```bash
# Create a stream
frkrcfg stream create my-api \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --tenant="default" \
  --description="My API stream" \
  --retention-days=7

# List streams
frkrcfg stream list \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --tenant="default"

# Get stream details
frkrcfg stream get my-api \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --tenant="default"

# Create a user
frkrcfg user create testuser \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --tenant="default"
```

### migrate - Database Migrations

```bash
frkrcfg migrate \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --migrations-path="../../frkr-common/migrations"
```

**Note:** `frkrup` automatically runs migrations during setup, so you typically don't need to run this manually.

### Migration Sync for Helm Charts

The Helm deployment process automatically syncs database migrations from `frkr-common/migrations/` to `frkr-infra-helm/migrations/` before deployment. This is handled automatically by:

- **`frkrup`**: Syncs migrations before Helm chart installation
- **`make deploy`**: The `deploy` target depends on `sync-migrations`, which uses Go module resolution to locate `frkr-common` and copy migration files

Migration files are synced using the `frkr-common/paths` package, which uses Go's module resolution (`go list -m`) to find the `frkr-common` module location. This ensures migrations are always up-to-date and eliminates the need for manual copying.

## Testing

The test suite uses testcontainers to spin up a real CockroachDB instance for integration testing. Tests require Docker to be running.

```bash
# Run all tests
make test

# Run tests with coverage report
make test-coverage

# Run tests with verbose output
go test ./... -v

# Run specific test package
go test ./pkg/db/... -v

# Run specific test
go test ./pkg/db/... -v -run TestCreateStream
```

### E2E Tests for Kubernetes

End-to-end tests verify that `frkrup`'s Kubernetes external access configurations work correctly (LoadBalancer, Ingress, ClusterIP). These tests use kind clusters with MetalLB and nginx Ingress controller.

**Prerequisites:** kind, kubectl, helm, Docker, and git submodules initialized

```bash
# Run all E2E tests
make test-e2e

# Or run directly
go test -tags=e2e ./cmd/frkrup/... -v -run TestKubernetesExternalAccess

# Run specific test
go test -tags=e2e ./cmd/frkrup/... -v -run TestKubernetesExternalAccess_LoadBalancer
```

**What the tests verify:**
- **LoadBalancer**: Services patched to LoadBalancer, MetalLB assigns IPs, gateways accessible via HTTP
- **Ingress**: Ingress resource created, nginx controller routes traffic, gateways accessible via paths
- **ClusterIP**: Services remain internal-only, verified via port-forwarding

Tests are excluded from regular test runs (use `-tags=e2e`). Each test creates a kind cluster, installs required components, and verifies actual HTTP connectivity.

## Development

### Code Quality

- Code is automatically formatted with `go fmt`
- Linting is performed with `go vet`
- All code changes should pass `make verify` before committing

### Project Structure

```
frkr-tools/
├── cmd/
│   ├── frkrcfg/          # Configuration CLI
│   │   ├── main.go
│   │   ├── stream.go
│   │   ├── user.go
│   │   └── migrate.go
│   └── frkrup/           # Interactive setup tool
│       ├── main.go        # Orchestration
│       ├── config.go      # Configuration struct
│       ├── prompt.go      # User interaction
│       ├── paths.go       # Path resolution
│       ├── infrastructure.go # Docker Compose & infrastructure
│       ├── database.go    # Database operations
│       ├── broker.go      # Broker operations
│       ├── gateway.go     # Gateway management
│       ├── kubernetes.go  # Kubernetes deployment
│       └── cleanup.go     # Cleanup operations
├── pkg/
│   └── db/               # Database operations
├── frkr-ingest-gateway/  # Git submodule
├── frkr-streaming-gateway/ # Git submodule
├── frkr-infra-helm/      # Git submodule
├── frkr-infra-docker/    # Git submodule
├── bin/                   # Build output (gitignored)
├── Makefile
├── README.md
├── QUICKSTART.md         # Local Docker Compose guide
└── K8S-QUICKSTART.md     # Kubernetes deployment guide
```

## License

Apache 2.0

