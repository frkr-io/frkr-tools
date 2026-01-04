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
# Build binary to bin/frkrcfg
make build

# Run tests
make test

# Run tests with coverage report
make test-coverage

# Format code
make fmt

# Run linter
make vet

# Run all verification checks
make verify

# Clean build artifacts
make clean

# Install to GOPATH/bin
make install
```

#### Using Build Script

```bash
# Build all binaries to bin/
./build.sh

# Or using Go
go run build.go
```

#### Manual Build

```bash
# Build a specific binary
go build -o bin/frkrcfg ./cmd/frkrcfg

# Build all binaries (using script)
./build.sh

# Run tests
go test ./...

# Run tests with coverage
go test ./... -coverprofile cover.out
go tool cover -html=cover.out -o cover.html
```

### Binary Location

The build process places binaries in the `bin/` directory:
- `bin/frkrcfg` - Main configuration tool

## Installation

### From Source

```bash
# Clone the repository with submodules
git clone --recurse-submodules https://github.com/frkr-io/frkr-tools.git
cd frkr-tools

# If you already cloned without submodules, initialize them:
git submodule update --init --recursive

# Build
make build

# Binary will be in bin/frkrcfg
```

**Note**: `frkr-tools` uses git submodules for:
- `frkr-ingest-gateway` and `frkr-streaming-gateway` (required by `frkrup` for local gateway startup)
- `frkr-infra-helm` (required by `frkrup` for Kubernetes deployment)
- `frkr-infra-docker` (optional, enables `frkrup` to automatically start Docker Compose services)

Make sure to clone with `--recurse-submodules` or initialize submodules manually.

### Using go install

```bash
go install github.com/frkr-io/frkr-tools/cmd/frkrcfg@latest
```

## Usage

### frkrcfg

```bash
# Create a stream
frkrcfg stream create my-api \
  --db-url="cockroachdb://root@localhost:26257/frkr?sslmode=disable" \
  --tenant="default" \
  --description="My API stream" \
  --retention-days=7

# List streams
frkrcfg stream list \
  --db-url="cockroachdb://root@localhost:26257/frkr?sslmode=disable" \
  --tenant="default"

# Get stream details
frkrcfg stream get my-api \
  --db-url="cockroachdb://root@localhost:26257/frkr?sslmode=disable" \
  --tenant="default"

# Create a user
frkrcfg user create testuser \
  --db-url="cockroachdb://root@localhost:26257/frkr?sslmode=disable" \
  --tenant="default"
```

### migrate

```bash
frkrcfg migrate \
  --db-url="cockroachdb://root@localhost:26257/frkr?sslmode=disable" \
  --migrations-path="../../frkr-common/migrations"
```

## Testing

The test suite uses testcontainers to spin up a real CockroachDB instance for integration testing. Tests require Docker to be running.

```bash
# Run all tests
make test

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

