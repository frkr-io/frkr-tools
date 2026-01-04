#!/bin/bash
# Build script that builds all binaries to bin/ directory
# Can be run as: ./build.sh or go run build.go (if we create a Go version)

set -e

BIN_DIR="bin"
CMD_DIR="cmd"

# Create bin directory if it doesn't exist
mkdir -p "$BIN_DIR"

# Find all command directories
for cmd_path in "$CMD_DIR"/*/; do
    if [ -d "$cmd_path" ]; then
        cmd_name=$(basename "$cmd_path")
        echo "Building $cmd_name..."
        go build -o "$BIN_DIR/$cmd_name" "./$cmd_path"
        echo "✅ Built $cmd_name -> $BIN_DIR/$cmd_name"
    fi
done

echo ""
echo "All binaries built successfully in $BIN_DIR/"

