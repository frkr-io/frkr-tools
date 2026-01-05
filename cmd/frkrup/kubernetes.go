package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// KubernetesManager handles Kubernetes setup operations
type KubernetesManager struct {
	config *Config
}

// NewKubernetesManager creates a new KubernetesManager
func NewKubernetesManager(config *Config) *KubernetesManager {
	return &KubernetesManager{config: config}
}

// Setup performs the full Kubernetes setup
func (km *KubernetesManager) Setup() error {
	fmt.Println("\n🚀 Setting up frkr on Kubernetes...")

	// Check kubectl
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH")
	}

	// Check helm
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH")
	}

	// Verify kubectl is connected to a cluster
	fmt.Println("\n🔍 Verifying Kubernetes cluster connection...")
	cmd := exec.Command("kubectl", "cluster-info")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl not connected to a cluster. Please create a cluster first (e.g., 'kind create cluster --name <cluster-name>')")
	}
	fmt.Println("✅ Connected to Kubernetes cluster")

	// Get cluster name if not provided
	if err := km.determineClusterName(); err != nil {
		return err
	}
	fmt.Printf("Using cluster: %s\n", km.config.K8sClusterName)

	// Get repo root (assume we're in frkr-tools)
	repoRoot, err := filepath.Abs("../")
	if err != nil {
		return fmt.Errorf("failed to get repo root: %w", err)
	}

	// Build and load gateway images
	if err := km.buildAndLoadImages(); err != nil {
		return err
	}

	// Install helm chart
	if err := km.installHelmChart(); err != nil {
		return err
	}

	// Wait for pods
	if err := km.waitForPods(); err != nil {
		return err
	}

	// Port forward
	portForwardCmds, err := km.setupPortForwarding()
	if err != nil {
		return err
	}
	defer func() {
		fmt.Println("\n🛑 Stopping port forwarding...")
		for _, cmd := range portForwardCmds {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}()

	// Run migrations
	fmt.Println("\n🗄️  Running database migrations...")
	if err := RunMigrationsK8s(repoRoot); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	// Verify gateways
	fmt.Println("\n✅ Verifying gateways...")
	time.Sleep(2 * time.Second)
	gatewayMgr := NewGatewaysManager(km.config)
	if err := gatewayMgr.VerifyGateways(km.config.IngestPort, km.config.StreamingPort); err != nil {
		return fmt.Errorf("gateway verification failed: %w", err)
	}

	fmt.Println("\n✅ frkr is running on Kubernetes!")
	fmt.Printf("   Ingest Gateway: http://localhost:%d\n", km.config.IngestPort)
	fmt.Printf("   Streaming Gateway: http://localhost:%d\n", km.config.StreamingPort)
	fmt.Println("\nPress Ctrl+C to stop port forwarding and exit.")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	return nil
}

func (km *KubernetesManager) determineClusterName() error {
	if km.config.K8sClusterName != "" {
		return nil
	}

	// Try to get cluster name from kubectl context
	ctxCmd := exec.Command("kubectl", "config", "current-context")
	ctxOutput, err := ctxCmd.Output()
	if err == nil {
		ctxStr := strings.TrimSpace(string(ctxOutput))
		// For kind clusters, context is usually "kind-<cluster-name>"
		if strings.HasPrefix(ctxStr, "kind-") {
			km.config.K8sClusterName = strings.TrimPrefix(ctxStr, "kind-")
			return nil
		}
	}

	// Prompt for cluster name
	fmt.Print("Kubernetes cluster name: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	km.config.K8sClusterName = strings.TrimSpace(scanner.Text())
	if km.config.K8sClusterName == "" {
		return fmt.Errorf("cluster name is required")
	}

	return nil
}

func (km *KubernetesManager) buildAndLoadImages() error {
	fmt.Println("\n📦 Building and loading gateway images...")

	ingestGatewayPath, err := findGatewayRepoPath("ingest")
	if err != nil {
		return fmt.Errorf("failed to find ingest gateway: %w", err)
	}
	if err := km.buildAndLoadImage(ingestGatewayPath, "frkr-ingest-gateway:0.1.0"); err != nil {
		return fmt.Errorf("failed to build ingest gateway: %w", err)
	}

	streamingGatewayPath, err := findGatewayRepoPath("streaming")
	if err != nil {
		return fmt.Errorf("failed to find streaming gateway: %w", err)
	}
	if err := km.buildAndLoadImage(streamingGatewayPath, "frkr-streaming-gateway:0.1.0"); err != nil {
		return fmt.Errorf("failed to build streaming gateway: %w", err)
	}

	return nil
}

func (km *KubernetesManager) buildAndLoadImage(path, imageName string) error {
	// Check for Dockerfile
	dockerfile := filepath.Join(path, "Dockerfile")
	if _, err := os.Stat(dockerfile); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found at %s", dockerfile)
	}

	// Build image
	fmt.Printf("  Building %s...\n", imageName)
	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfile, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Load into kind cluster
	fmt.Printf("  Loading %s into kind cluster '%s'...\n", imageName, km.config.K8sClusterName)
	cmd = exec.Command("kind", "load", "docker-image", imageName, "--name", km.config.K8sClusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind load failed (make sure kind cluster exists): %w", err)
	}

	return nil
}

func (km *KubernetesManager) installHelmChart() error {
	fmt.Println("\n📥 Installing frkr Helm chart...")
	helmPath, err := findInfraRepoPath("helm")
	if err != nil {
		return fmt.Errorf("failed to find frkr-infra-helm: %w", err)
	}
	helmCmd := exec.Command("helm", "install", "frkr", ".", "-f", "values-full.yaml")
	helmCmd.Dir = helmPath
	helmCmd.Stdout = os.Stdout
	helmCmd.Stderr = os.Stderr
	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm install failed: %w", err)
	}
	return nil
}

func (km *KubernetesManager) waitForPods() error {
	fmt.Println("\n⏳ Waiting for pods to be ready...")
	waitPods := []string{
		"app.kubernetes.io/component=operator",
		"app.kubernetes.io/component=ingest-gateway",
		"app.kubernetes.io/component=streaming-gateway",
	}

	for _, selector := range waitPods {
		cmd := exec.Command("kubectl", "wait", "--for=condition=ready", "pod", "-l", selector, "--timeout=300s")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pod not ready: %s", selector)
		}
	}
	return nil
}

func (km *KubernetesManager) setupPortForwarding() ([]*exec.Cmd, error) {
	fmt.Println("\n🔌 Setting up port forwarding...")
	portForwards := []struct {
		service string
		local   string
		remote  string
	}{
		{"frkr-ingest-gateway", fmt.Sprintf("%d", km.config.IngestPort), "8080"},
		{"frkr-streaming-gateway", fmt.Sprintf("%d", km.config.StreamingPort), "8081"},
	}

	portForwardCmds := []*exec.Cmd{}
	for _, pf := range portForwards {
		cmd := exec.Command("kubectl", "port-forward", fmt.Sprintf("svc/%s", pf.service), fmt.Sprintf("%s:%s", pf.local, pf.remote))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			// Kill any already started port forwards
			for _, c := range portForwardCmds {
				if c.Process != nil {
					c.Process.Kill()
				}
			}
			return nil, fmt.Errorf("port forward failed for %s: %w", pf.service, err)
		}
		portForwardCmds = append(portForwardCmds, cmd)
		fmt.Printf("✅ Port forwarding %s:%s -> %s\n", pf.local, pf.remote, pf.service)
	}

	return portForwardCmds, nil
}
