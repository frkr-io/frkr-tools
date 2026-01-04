//go:build ignore
// +build ignore

// Build script: Run with "go run build.go" to build all binaries to bin/
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	binDir := "bin"
	cmdDir := "cmd"

	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating bin directory: %v\n", err)
		os.Exit(1)
	}

	// Find all command directories
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading cmd directory: %v\n", err)
		os.Exit(1)
	}

	built := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		cmdName := entry.Name()
		cmdPath := filepath.Join(cmdDir, cmdName)
		outputPath := filepath.Join(binDir, cmdName)

		fmt.Printf("Building %s...\n", cmdName)

		// Build the binary
		cmd := exec.Command("go", "build", "-o", outputPath, "./"+cmdPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error building %s: %v\n", cmdName, err)
			os.Exit(1)
		}

		fmt.Printf("✅ Built %s -> %s\n", cmdName, outputPath)
		built++
	}

	if built == 0 {
		fmt.Fprintf(os.Stderr, "No command directories found in %s/\n", cmdDir)
		os.Exit(1)
	}

	fmt.Printf("\n✅ All %d binaries built successfully in %s/\n", built, binDir)
}

