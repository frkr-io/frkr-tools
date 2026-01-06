package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// GatewaysManager handles gateway operations for both ingest and streaming gateways
type GatewaysManager struct {
	config *Config
}

// NewGatewaysManager creates a new GatewaysManager
func NewGatewaysManager(config *Config) *GatewaysManager {
	return &GatewaysManager{config: config}
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

	cmd := exec.CommandContext(ctx, "go", "run", mainFile)
	cmd.Dir = gatewayDir
	cmd.Args = append(cmd.Args,
		"--http-port", fmt.Sprintf("%d", port),
		"--db-url", dbURL,
		"--broker-url", brokerURL)

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
func (gm *GatewaysManager) VerifyGateways(ingestPort, streamingPort int) error {
	// Check ingest gateway
	fmt.Printf("   Checking ingest gateway (http://localhost:%d/health)...\n", ingestPort)
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", ingestPort))
	if err != nil {
		return fmt.Errorf("ingest gateway health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest gateway returned status %d (expected 200)", resp.StatusCode)
	}
	fmt.Printf("   ✅ Ingest gateway is healthy\n")

	// Check streaming gateway
	fmt.Printf("   Checking streaming gateway (http://localhost:%d/health)...\n", streamingPort)
	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/health", streamingPort))
	if err != nil {
		return fmt.Errorf("streaming gateway health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("streaming gateway returned status %d (expected 200)", resp.StatusCode)
	}
	fmt.Printf("   ✅ Streaming gateway is healthy\n")

	return nil
}

// VerifyGatewaysWithRetries verifies gateways with retry logic
func (gm *GatewaysManager) VerifyGatewaysWithRetries(ingestPort, streamingPort int, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := gm.VerifyGateways(ingestPort, streamingPort); err != nil {
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
