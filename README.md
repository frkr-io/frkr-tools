# frkr-tools

Local development tools for frkr.

## Purpose

frkr-tools provides direct database and configuration access for local development, without requiring Kubernetes. This is separate from `frkrctl` which is k8s-focused.

## Tools

- **frkrcfg**: Direct configuration tool (streams, tenants, users)
- **frkrup**: Interactive setup tool for local or Kubernetes deployment
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

### frkrcfg

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

### migrate

```bash
frkrcfg migrate \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --migrations-path="../../frkr-common/migrations"
```

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
│       └── main.go
├── pkg/
│   └── db/               # Database operations
├── frkr-ingest-gateway/  # Git submodule
├── frkr-streaming-gateway/ # Git submodule
├── frkr-infra-helm/      # Git submodule
├── frkr-infra-docker/    # Git submodule
├── bin/                   # Build output (gitignored)
├── Makefile
└── README.md
```

## License

Apache 2.0

