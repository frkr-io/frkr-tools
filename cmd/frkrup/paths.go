package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findMigrationsPath uses Go module resolution to find frkr-common/migrations
// Works with both local (replace directive) and remote dependencies
func findMigrationsPath() (string, error) {
	// Use Go's module resolution - respects replace directives and module cache
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/frkr-io/frkr-common")
	output, err := cmd.Output()
	if err == nil {
		moduleDir := strings.TrimSpace(string(output))
		migrationsPath := filepath.Join(moduleDir, "migrations")
		if _, err := os.Stat(migrationsPath); err == nil {
			return migrationsPath, nil
		}
	}

	// Fallback: try local path (for development when go.mod isn't set up yet)
	repoRoot, _ := filepath.Abs("../")
	localPath := filepath.Join(repoRoot, "frkr-common", "migrations")
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	return "", fmt.Errorf("migrations not found: frkr-common module not found via 'go list -m' and local path %s does not exist", localPath)
}

// findGatewayRepoPath finds the gateway repository root path using git submodules.
// It automatically initializes submodules if needed.
func findGatewayRepoPath(gatewayType string) (string, error) {
	// Get frkr-tools directory (where we are running from)
	frkrupDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Find frkr-tools root by looking for .gitmodules
	toolsRoot := frkrupDir
	for {
		gitmodulesPath := filepath.Join(toolsRoot, ".gitmodules")
		if _, err := os.Stat(gitmodulesPath); err == nil {
			break
		}
		parent := filepath.Dir(toolsRoot)
		if parent == toolsRoot {
			return "", fmt.Errorf("frkr-tools root not found (no .gitmodules)")
		}
		toolsRoot = parent
	}

	var gatewayName string
	if gatewayType == "ingest" {
		gatewayName = "frkr-ingest-gateway"
	} else if gatewayType == "streaming" {
		gatewayName = "frkr-streaming-gateway"
	} else {
		return "", fmt.Errorf("unknown gateway type: %s", gatewayType)
	}

	submodulePath := filepath.Join(toolsRoot, gatewayName)
	goModPath := filepath.Join(submodulePath, "go.mod")

	// Check if submodule exists and is initialized
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		// Try to initialize submodule
		fmt.Printf("  Initializing submodule %s...\n", gatewayName)
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", submodulePath)
		cmd.Dir = toolsRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to initialize submodule %s: %w\n\nHint: Make sure you cloned frkr-tools with --recurse-submodules, or run: git submodule update --init --recursive", gatewayName, err)
		}
	}

	// Verify submodule is now available
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return "", fmt.Errorf("gateway %s not found at %s\n\nHint: Run 'git submodule update --init --recursive' in frkr-tools", gatewayName, submodulePath)
	}

	return submodulePath, nil
}

// findGatewayPath finds the gateway cmd directory path using git submodules.
// It automatically initializes submodules if needed.
func findGatewayPath(gatewayType string) (string, error) {
	repoPath, err := findGatewayRepoPath(gatewayType)
	if err != nil {
		return "", err
	}

	// Return path to cmd/gateway directory
	cmdPath := filepath.Join(repoPath, "cmd", "gateway")
	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		return "", fmt.Errorf("gateway cmd directory not found at %s", cmdPath)
	}

	return cmdPath, nil
}

// findInfraRepoPath finds the infrastructure repository path using git submodules.
// It automatically initializes submodules if needed.
func findInfraRepoPath(infraType string) (string, error) {
	// Get frkr-tools directory (where we are running from)
	frkrupDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Find frkr-tools root by looking for .gitmodules
	toolsRoot := frkrupDir
	for {
		gitmodulesPath := filepath.Join(toolsRoot, ".gitmodules")
		if _, err := os.Stat(gitmodulesPath); err == nil {
			break
		}
		parent := filepath.Dir(toolsRoot)
		if parent == toolsRoot {
			return "", fmt.Errorf("frkr-tools root not found (no .gitmodules)")
		}
		toolsRoot = parent
	}

	var repoName string
	switch infraType {
	case "helm":
		repoName = "frkr-infra-helm"
	case "docker":
		repoName = "frkr-infra-docker"
	default:
		return "", fmt.Errorf("unknown infra type: %s", infraType)
	}

	submodulePath := filepath.Join(toolsRoot, repoName)

	// Check if submodule exists and is initialized
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		// Try to initialize submodule
		fmt.Printf("  Initializing submodule %s...\n", repoName)
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", submodulePath)
		cmd.Dir = toolsRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to initialize submodule %s: %w\n\nHint: Make sure you cloned frkr-tools with --recurse-submodules, or run: git submodule update --init --recursive", repoName, err)
		}
	}

	// Verify submodule is now available
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		return "", fmt.Errorf("infrastructure repository %s not found at %s\n\nHint: Run 'git submodule update --init --recursive' in frkr-tools", repoName, submodulePath)
	}

	return submodulePath, nil
}
