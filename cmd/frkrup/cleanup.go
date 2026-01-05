package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxKillRetries          = 3
	gracefulShutdownTimeout = 3 * time.Second
	killRetryInterval       = 1 * time.Second
	overallCleanupTimeout   = 15 * time.Second
	dockerCleanupTimeout    = 30 * time.Second
)

// CleanupManager handles cleanup operations
type CleanupManager struct {
	config *Config
}

// NewCleanupManager creates a new CleanupManager
func NewCleanupManager(config *Config) *CleanupManager {
	return &CleanupManager{config: config}
}

// CleanupDocker stops Docker Compose services if we started them
func (cm *CleanupManager) CleanupDocker() bool {
	if !cm.config.StartedDocker {
		return true
	}
	fmt.Println("🛑 Stopping Docker Compose services...")
	dockerPath, err := findInfraRepoPath("docker")
	if err != nil {
		fmt.Printf("⚠️  Could not find docker path: %v\n", err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerCleanupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "down")
	cmd.Dir = dockerPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("⚠️  Docker compose down failed: %v\n", err)
		// Try force kill as fallback
		cmd2 := exec.CommandContext(ctx, "docker", "compose", "kill")
		cmd2.Dir = dockerPath
		cmd2.Run()
		return false
	}

	cm.config.StartedDocker = false
	return true
}

// CleanupLocal performs full cleanup for local setup with concurrent operations and timeouts
func (cm *CleanupManager) CleanupLocal() {
	if !cm.hasAnythingToCleanup() {
		return
	}

	fmt.Println("\n🧹 Cleaning up...")

	ctx, cancel := context.WithTimeout(context.Background(), overallCleanupTimeout)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan string, 4)

	// Kill gateway processes concurrently
	cm.cleanupGateway(ctx, &wg, results, cm.config.IngestCmd, "ingest gateway", func() {
		cm.config.IngestCmd = nil
	})
	cm.cleanupGateway(ctx, &wg, results, cm.config.StreamingCmd, "streaming gateway", func() {
		cm.config.StreamingCmd = nil
	})

	// Kill processes on ports concurrently (fallback)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cm.killPortProcesses(ctx, cm.config.IngestPort, "ingest")
		cm.killPortProcesses(ctx, cm.config.StreamingPort, "streaming")
		results <- "✅ Port cleanup completed"
	}()

	// Stop Docker Compose (always run, even if gateways hang)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if cm.CleanupDocker() {
			results <- "✅ Docker Compose stopped"
		} else {
			results <- "⚠️  Docker Compose cleanup had issues"
		}
	}()

	// Wait for all operations with timeout
	cm.waitForCleanup(ctx, results, &wg)

	fmt.Println("✅ Cleanup complete")
}

// hasAnythingToCleanup checks if there's anything to clean up
func (cm *CleanupManager) hasAnythingToCleanup() bool {
	return cm.config.StartedDocker || cm.config.IngestCmd != nil || cm.config.StreamingCmd != nil
}

// cleanupGateway handles cleanup of a single gateway process
func (cm *CleanupManager) cleanupGateway(ctx context.Context, wg *sync.WaitGroup, results chan<- string, cmd *exec.Cmd, name string, onComplete func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		if cmd != nil {
			success := cm.killProcessWithTimeout(ctx, cmd, name)
			if success {
				results <- fmt.Sprintf("✅ %s stopped", name)
			} else {
				results <- fmt.Sprintf("⚠️  %s cleanup timed out", name)
			}
			onComplete()
		}
	}()
}

// waitForCleanup waits for all cleanup operations to complete or timeout
func (cm *CleanupManager) waitForCleanup(ctx context.Context, results chan string, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-ctx.Done():
		// Timeout - report what we know
		fmt.Println("⚠️  Cleanup timeout - some operations may still be running")
	}

	close(results)
	for result := range results {
		fmt.Printf("   %s\n", result)
	}
}

// killProcessWithTimeout attempts to gracefully kill a process with timeout and retries
func (cm *CleanupManager) killProcessWithTimeout(ctx context.Context, cmd *exec.Cmd, name string) bool {
	if cmd == nil || cmd.Process == nil {
		return true
	}

	pid := cmd.Process.Pid
	fmt.Printf("🛑 Stopping %s (PID %d)...\n", name, pid)

	// Try graceful shutdown first (SIGTERM)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("⚠️  Failed to send SIGTERM to %s: %v\n", name, err)
		return cm.killProcessWithRetries(ctx, pid, name)
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Verify process is actually dead
		if cm.isProcessRunning(pid) {
			fmt.Printf("   ⚠️  %s reported stopped but PID still exists, force killing...\n", name)
			return cm.killProcessWithRetries(ctx, pid, name)
		}
		fmt.Printf("   ✅ %s stopped gracefully\n", name)
		return true
	case <-time.After(gracefulShutdownTimeout):
		// Force kill if still running
		fmt.Printf("   ⚠️  %s didn't stop gracefully, force killing...\n", name)
		return cm.killProcessWithRetries(ctx, pid, name)
	case <-ctx.Done():
		// Overall timeout - force kill with retries
		return cm.killProcessWithRetries(ctx, pid, name)
	}
}

// killProcessWithRetries kills a process by PID with retries and verification
func (cm *CleanupManager) killProcessWithRetries(ctx context.Context, pid int, name string) bool {
	for attempt := 1; attempt <= maxKillRetries; attempt++ {
		if !cm.isProcessRunning(pid) {
			fmt.Printf("   ✅ %s (PID %d) is no longer running\n", name, pid)
			return true
		}

		fmt.Printf("   🛑 Attempt %d/%d: Killing PID %d...\n", attempt, maxKillRetries, pid)

		if !cm.attemptKillProcess(ctx, pid, name) {
			continue
		}

		// Wait and verify process is dead
		select {
		case <-time.After(killRetryInterval):
			if !cm.isProcessRunning(pid) {
				fmt.Printf("   ✅ %s (PID %d) killed successfully\n", name, pid)
				return true
			}
			if attempt < maxKillRetries {
				fmt.Printf("   ⚠️  PID %d still running, retrying...\n", pid)
			}
		case <-ctx.Done():
			fmt.Printf("   ⚠️  Timeout while killing %s (PID %d)\n", name, pid)
			return false
		}
	}

	// Final check
	if cm.isProcessRunning(pid) {
		fmt.Printf("   ❌ %s (PID %d) still running after %d attempts\n", name, pid, maxKillRetries)
		return false
	}

	fmt.Printf("   ✅ %s (PID %d) killed after retries\n", name, pid)
	return true
}

// attemptKillProcess attempts to kill a process and returns true if the attempt was made
func (cm *CleanupManager) attemptKillProcess(ctx context.Context, pid int, name string) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("   ⚠️  Could not find process %d: %v\n", pid, err)
		return !cm.isProcessRunning(pid) // Return true if process is actually gone
	}

	if err := process.Signal(syscall.SIGKILL); err != nil {
		fmt.Printf("   ⚠️  Failed to send SIGKILL to PID %d: %v\n", pid, err)
		return !cm.isProcessRunning(pid) // Return true if process is actually gone
	}

	return true
}

// isProcessRunning checks if a process with the given PID is still running
func (cm *CleanupManager) isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists (doesn't actually send a signal)
	return process.Signal(syscall.Signal(0)) == nil
}

// killPortProcesses kills any processes listening on the given port
func (cm *CleanupManager) killPortProcesses(ctx context.Context, port int, name string) {
	cmd := exec.CommandContext(ctx, "lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		// No process on port - that's fine
		return
	}

	pids := strings.Fields(string(output))
	for _, pidStr := range pids {
		if pidStr == "" {
			continue
		}
		fmt.Printf("   🛑 Killing process %s on %s port (%d)...\n", pidStr, name, port)
		killCmd := exec.CommandContext(ctx, "kill", "-9", pidStr)
		if err := killCmd.Run(); err != nil {
			fmt.Printf("   ⚠️  Failed to kill process %s: %v\n", pidStr, err)
		}
	}
}

// Convenience functions for backward compatibility

// killProcess is a convenience function for backward compatibility
func killProcess(cmd *exec.Cmd) {
	cm := NewCleanupManager(&Config{})
	ctx := context.Background()
	cm.killProcessWithTimeout(ctx, cmd, "process")
}

// cleanupDocker is a convenience function for backward compatibility
func cleanupDocker(config *Config) {
	cm := NewCleanupManager(config)
	cm.CleanupDocker()
}

// cleanupLocal is a convenience function for backward compatibility
func cleanupLocal(config *Config) {
	cm := NewCleanupManager(config)
	cm.CleanupLocal()
}
