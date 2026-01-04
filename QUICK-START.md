# Quick Start Guide

## One-Time Setup

### 1. Install Prerequisites

```bash
# Go 1.21+ (required for frkr-operator)
go version  # Should show 1.21 or higher

# controller-gen (required for frkr-operator)
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
export PATH=$PATH:$(go env GOPATH)/bin

# Verify
controller-gen --version
```

**Permanent Setup**: Add to `~/.bashrc` or `~/.zshrc`:
```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### 2. Build All Repositories

```bash
cd ~/git/frkr-io

# frkr-common (foundation)
cd frkr-common
go mod tidy && go build ./... && go test ./...
cd ..

# frkr-operator (requires code generation)
cd frkr-operator
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
go build ./cmd/operator/...
cd ..

# frkr-ingest-gateway
cd frkr-ingest-gateway
go mod tidy && go build ./cmd/gateway/...
cd ..

# frkrctl
cd frkrctl
go mod tidy && go build ./cmd/frkrctl/...
cd ..
```

## Common Issues

### "controller-gen: command not found"
```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
export PATH=$PATH:$(go env GOPATH)/bin
```

### "found packages v1 and hack" (frkr-operator)
```bash
cd frkr-operator
# Regenerate code
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
# Check api/v1/zz_generated.deepcopy.go has "package v1" (not "package hack")
```

### "use of internal package not allowed" (Historical - Resolved)
- Plugins package is public: `github.com/frkr-io/frkr-common/plugins`
- Note: `internal/` folder was removed from `frkr-common` - all code is now in public packages

### "package cmp is not in GOROOT"
- frkr-operator requires Go 1.21+
- Upgrade Go or use Go 1.21+ for all repos

## Detailed Documentation

- **Main Guide**: [frkr-io-planning-docs/BUILD-AND-TEST.md](frkr-io-planning-docs/BUILD-AND-TEST.md)
- **frkr-operator**: [frkr-operator/BUILD.md](frkr-operator/BUILD.md)
- **frkr-common**: [frkr-common/BUILD.md](frkr-common/BUILD.md)
- **frkr-ingest-gateway**: [frkr-ingest-gateway/BUILD.md](frkr-ingest-gateway/BUILD.md)

