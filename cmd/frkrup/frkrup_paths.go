package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	frkrcommonpaths "github.com/frkr-io/frkr-common/paths"
)

// isKubernetesAvailable checks if kubectl is available and connected to a cluster
func isKubernetesAvailable() bool {
	// Check if kubectl exists
	if _, err := exec.LookPath("kubectl"); err != nil {
		return false
	}
	
	// Check if kubectl can connect to a cluster
	cmd := exec.Command("kubectl", "cluster-info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// isKindCluster checks if the current kubectl context is a kind cluster
func isKindCluster() bool {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return false
	}
	
	// Get current context
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	
	ctxStr := strings.TrimSpace(string(output))
	// Kind clusters typically have context names starting with "kind-"
	return strings.HasPrefix(ctxStr, "kind-")
}

// findMigrationsPath uses frkr-common/paths package to find migrations directory
func findMigrationsPath() (string, error) {
	return frkrcommonpaths.MigrationsPath()
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

	var repoName string
	if gatewayType == "ingest" {
		repoName = "frkr-ingest-gateway"
	} else if gatewayType == "streaming" {
		repoName = "frkr-streaming-gateway"
	} else if gatewayType == "operator" {
		repoName = "frkr-operator"
	} else {
		return "", fmt.Errorf("unknown component type: %s", gatewayType)
	}


	submodulePath := filepath.Join(toolsRoot, repoName)
	goModPath := filepath.Join(submodulePath, "go.mod")

	// Check if submodule exists and is initialized
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
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
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return "", fmt.Errorf("component %s not found at %s\n\nHint: Run 'git submodule update --init --recursive' in frkr-tools", repoName, submodulePath)
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
