package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
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
func (cm *CleanupManager) CleanupDocker() {
	if !cm.config.StartedDocker {
		return
	}
	fmt.Println("\n🛑 Stopping Docker Compose services...")
	dockerPath, err := findInfraRepoPath("docker")
	if err == nil {
		cmd := exec.Command("docker", "compose", "down")
		cmd.Dir = dockerPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run() // Ignore errors - services might already be stopped
	}
	cm.config.StartedDocker = false
}

// CleanupLocal performs full cleanup for local setup
func (cm *CleanupManager) CleanupLocal() {
	// Check if there's anything to clean up
	if !cm.config.StartedDocker && cm.config.IngestCmd == nil && cm.config.StreamingCmd == nil {
		return
	}

	fmt.Println("\n🧹 Cleaning up...")

	// Kill gateway processes first (most important cleanup)
	if cm.config.IngestCmd != nil {
		fmt.Println("🛑 Stopping ingest gateway...")
		killProcess(cm.config.IngestCmd)
		cm.config.IngestCmd = nil
	}
	if cm.config.StreamingCmd != nil {
		fmt.Println("🛑 Stopping streaming gateway...")
		killProcess(cm.config.StreamingCmd)
		cm.config.StreamingCmd = nil
	}

	// Also try to kill any processes on the gateway ports (fallback)
	fmt.Println("🛑 Checking for processes on gateway ports...")
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", cm.config.IngestPort))
	if output, err := cmd.Output(); err == nil {
		if pid := strings.TrimSpace(string(output)); pid != "" {
			fmt.Printf("   Killing process on ingest port: %s\n", pid)
			exec.Command("kill", "-9", pid).Run()
		}
	}
	cmd = exec.Command("lsof", "-ti", fmt.Sprintf(":%d", cm.config.StreamingPort))
	if output, err := cmd.Output(); err == nil {
		if pid := strings.TrimSpace(string(output)); pid != "" {
			fmt.Printf("   Killing process on streaming port: %s\n", pid)
			exec.Command("kill", "-9", pid).Run()
		}
	}

	// Stop Docker Compose if we started it (most important for failure cases)
	cm.CleanupDocker()

	fmt.Println("✅ Cleanup complete")
}

// killProcess attempts to gracefully kill a process, then force kills if needed
func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	fmt.Printf("🛑 Killing process %d...\n", cmd.Process.Pid)

	// Try graceful shutdown first (SIGTERM)
	cmd.Process.Signal(syscall.SIGTERM)

	// Wait a bit for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully
		return
	case <-time.After(2 * time.Second):
		// Force kill if still running
		cmd.Process.Kill()
		cmd.Wait()
	}
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
