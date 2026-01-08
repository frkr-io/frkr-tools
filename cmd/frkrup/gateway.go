package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GatewaysManager handles gateway operations for both ingest and streaming gateways
// Note: This is a thin orchestrator - health checking logic lives in the gateways themselves
type GatewaysManager struct {
	config *FrkrupConfig
}

// NewGatewaysManager creates a new GatewaysManager
func NewGatewaysManager(config *FrkrupConfig) *GatewaysManager {
	return &GatewaysManager{config: config}
}

// GatewayHealthResponse represents the structured health response from gateways
type GatewayHealthResponse struct {
	Status  string                 `json:"status"`
	Checks  map[string]CheckResult `json:"checks,omitempty"`
	Version string                 `json:"version,omitempty"`
	Uptime  string                 `json:"uptime,omitempty"`
}

// CheckResult represents a single health check from the gateway
type CheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// StartGateway starts a gateway process
func (gm *GatewaysManager) StartGateway(ctx context.Context, gatewayType string, port int, dbURL, brokerURL string) (*exec.Cmd, io.ReadCloser, io.ReadCloser) {
	gatewayPath, err := findGatewayPath(gatewayType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to find gateway: %v\n", err)
		return nil, nil, nil
	}

	// Get the directory containing the gateway main.go
	gatewayDir := gatewayPath
	mainFile := filepath.Join(gatewayDir, "main.go")

	// Debug: print what we're passing to the gateway
	fmt.Printf("   [DEBUG] Starting %s gateway:\n", gatewayType)
	fmt.Printf("   [DEBUG]   Dir: %s\n", gatewayDir)
	fmt.Printf("   [DEBUG]   DB_URL: %s\n", dbURL)
	fmt.Printf("   [DEBUG]   BROKER_URL: %s\n", brokerURL)

	cmd := exec.CommandContext(ctx, "go", "run", mainFile)
	cmd.Dir = gatewayDir
	// Set environment variables for gateways (these override flags in 12-factor pattern)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HTTP_PORT=%d", port),
		fmt.Sprintf("DB_URL=%s", dbURL),
		fmt.Sprintf("BROKER_URL=%s", brokerURL),
	)
	// Also pass as flags (gateway will prefer env vars if both are set)
	cmd.Args = append(cmd.Args,
		"--http-port", fmt.Sprintf("%d", port),
		"--db-url", dbURL,
		"--broker-url", brokerURL)
	
	// Debug: verify env vars are set
	fmt.Printf("   [DEBUG] Environment variables set:\n")
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "HTTP_PORT=") || strings.HasPrefix(env, "DB_URL=") || strings.HasPrefix(env, "BROKER_URL=") {
			fmt.Printf("   [DEBUG]   %s\n", env)
		}
	}
	fmt.Printf("   [DEBUG] Command args: %v\n", cmd.Args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return nil, nil, nil
	}

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, nil, nil
	}

	return cmd, stdout, stderr
}

// StreamLogs streams gateway logs to stdout/stderr
func (gm *GatewaysManager) StreamLogs(stdout, stderr io.ReadCloser, label string) {
	// Stream stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Printf("[%s] %s\n", label, scanner.Text())
		}
	}()

	// Stream stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", label, scanner.Text())
		}
	}()
}

// VerifyGateways verifies that both gateways are running and healthy
// This is now a thin wrapper - the gateways themselves check their dependencies
func (gm *GatewaysManager) VerifyGateways() error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Build URLs from config
	ingestURL := gm.config.BuildIngestGatewayURL()
	streamingURL := gm.config.BuildStreamingGatewayURL()

	// Check ingest gateway - gateways now return structured health with dependency status
	if err := checkGatewayHealth(client, "ingest", ingestURL); err != nil {
		return err
	}

	// Check streaming gateway
	if err := checkGatewayHealth(client, "streaming", streamingURL); err != nil {
		return err
	}

	return nil
}

// checkGatewayHealth checks a single gateway's health endpoint
// The gateway is responsible for checking its own dependencies (DB, broker)
func checkGatewayHealth(client *http.Client, name, url string) error {
	fmt.Printf("   Checking %s gateway at %s...\n", name, url)
	startTime := time.Now()
	
	resp, err := client.Get(url)
	duration := time.Since(startTime)
	
	if err != nil {
		return fmt.Errorf("%s gateway health check failed at %s: %w", name, url, err)
	}
	defer resp.Body.Close()

	// Parse structured health response from gateway
	var healthResp GatewayHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		// Gateway returned non-JSON (old format) - just check status code
		if resp.StatusCode == http.StatusOK {
			fmt.Printf("   ✅ %s gateway is healthy (took %v)\n", name, duration)
			return nil
		}
		return fmt.Errorf("%s gateway returned status %d (expected 200)", name, resp.StatusCode)
	}

	// Gateway now reports its own dependency status
	if resp.StatusCode != http.StatusOK || healthResp.Status != "healthy" {
		// Report which dependencies failed
		var failedChecks []string
		for checkName, check := range healthResp.Checks {
			if check.Status != "pass" {
				msg := checkName
				if check.Message != "" {
					msg += ": " + check.Message
				}
				failedChecks = append(failedChecks, msg)
			}
		}
		if len(failedChecks) > 0 {
			return fmt.Errorf("%s gateway unhealthy - failed checks: %v", name, failedChecks)
		}
		return fmt.Errorf("%s gateway returned unhealthy status: %s", name, healthResp.Status)
	}

	fmt.Printf("   ✅ %s gateway is healthy (v%s, uptime: %s, took %v)\n", 
		name, healthResp.Version, healthResp.Uptime, duration)
	return nil
}

// VerifyGatewaysWithRetries verifies gateways with retry logic
func (gm *GatewaysManager) VerifyGatewaysWithRetries(maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := gm.VerifyGateways(); err != nil {
			if i < maxRetries-1 {
				fmt.Printf("   ⏳ Retrying... (%d/%d)\n", i+1, maxRetries)
				time.Sleep(2 * time.Second)
				continue
			}
			fmt.Printf("   ❌ Gateway verification failed after %d attempts\n", maxRetries)
			return err
		}
		break
	}
	return nil
}
