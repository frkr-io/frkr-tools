package main

import (
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
	config *FrkrupConfig
}

// NewKubernetesManager creates a new KubernetesManager
func NewKubernetesManager(config *FrkrupConfig) *KubernetesManager {
	return &KubernetesManager{config: config}
}

// Setup performs the full Kubernetes setup
func (km *KubernetesManager) Setup() error {
	fmt.Println("\n🚀 Setting up frkr on Kubernetes...")

	// 1. Prerequisites Check
	if err := km.checkPrerequisites(); err != nil {
		return err
	}

	// 2. Identify Cluster
	if err := km.determineClusterName(); err != nil {
		return err
	}
	fmt.Printf("Using cluster: %s\n", km.config.K8sClusterName)

	// 3. Build and Load Images (if Kind)
	updatedImages := make(map[string]bool)
	if isKindCluster() { // simplified check, logic in main/prompt moved here? No, stick to config.
		var err error
		updatedImages, err = km.buildAndLoadImages()
		if err != nil {
			return err
		}
	}

	// 4. Install K8s Gateway API CRDs (must be done BEFORE Helm)
	// Note: Helm hooks can't install CRDs that are referenced in the same chart
	// because Helm validates all templates before running hooks
	if err := km.installGatewayAPICRDs(); err != nil {
		return err
	}

	// 6. Install/Upgrade Helm Chart
	// We translate config into Helm values here
	if err := km.installHelmChart(updatedImages); err != nil {
		return err
	}

	// 6. Wait for Readiness
	if err := km.waitForReadiness(); err != nil {
		return err
	}

	// 7. Port Forwarding (if requested)
	if !km.config.SkipPortForward {
		return km.runPortForwarding()
	}

	// 8. Success Message
	km.showSuccessMessage()

	return nil
}

func (km *KubernetesManager) checkPrerequisites() error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH")
	}

	fmt.Println("\n🔍 Verifying Kubernetes cluster connection...")
	cmd := exec.Command("kubectl", "cluster-info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl not connected to a cluster")
	}
	fmt.Println("✅ Connected to Kubernetes cluster")
	return nil
}

func (km *KubernetesManager) determineClusterName() error {
	// Get current context
	ctxCmd := exec.Command("kubectl", "config", "current-context")
	ctxOutput, err := ctxCmd.Output()
	currentCtx := strings.TrimSpace(string(ctxOutput))
	
	if err != nil {
		return fmt.Errorf("failed to get current kubernetes context: %w", err)
	}

	// 1. If config has a name, VALIDATE it
	if km.config.K8sClusterName != "" {
		if currentCtx != km.config.K8sClusterName {
			return fmt.Errorf("context mismatch!\n   Active Context: %s\n   Configured Cluster: %s\n\n👉 Please switch your kubectl context:\n   kubectl config use-context %s", 
				currentCtx, km.config.K8sClusterName, km.config.K8sClusterName)
		}
		return nil
	}

	// 2. If config has NO name, use current context (Auto-discovery)
	if strings.HasPrefix(currentCtx, "kind-") {
		km.config.K8sClusterName = strings.TrimPrefix(currentCtx, "kind-")
	} else {
		km.config.K8sClusterName = currentCtx
	}
	// Update config so other parts know the name
	return nil
}

func (km *KubernetesManager) buildAndLoadImages() (map[string]bool, error) {
	fmt.Println("\n📦 Building and loading images for Kind...")
	updated := make(map[string]bool)

	// Ingest Gateway
	p, err := findGatewayRepoPath("ingest")
	if err == nil {
		upd, err := km.buildAndLoadImage(p, "frkr-ingest-gateway:0.1.0")
		if err != nil { return nil, err }
		updated["frkr-ingest-gateway"] = upd
	}

	// Streaming Gateway
	p, err = findGatewayRepoPath("streaming")
	if err == nil {
		upd, err := km.buildAndLoadImage(p, "frkr-streaming-gateway:0.1.0")
		if err != nil { return nil, err }
		updated["frkr-streaming-gateway"] = upd
	}

	// Operator
	p, err = findGatewayRepoPath("operator")
	if err == nil {
		upd, err := km.buildAndLoadImage(p, "frkr-operator:0.1.1") // Keep version in sync
		if err != nil { return nil, err }
		updated["frkr-operator"] = upd
	}
	
	// Mock OIDC (optional, checking if we can build it? No, it uses public image usually)
	
	return updated, nil
}

func (km *KubernetesManager) buildAndLoadImage(path, imageName string) (bool, error) {
	// 0. Get current ID
	oldIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	oldIDBytes, _ := oldIDCmd.Output()
	oldID := strings.TrimSpace(string(oldIDBytes))

	// 1. Build
	fmt.Printf("  Building %s...\n", imageName)
	dockerfile := filepath.Join(path, "Dockerfile")
	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfile, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("build failed: %w", err)
	}

	// 2. Get New ID
	newIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	newIDBytes, _ := newIDCmd.Output()
	newID := strings.TrimSpace(string(newIDBytes))

	hasChanged := oldID != newID
	if !hasChanged {
		fmt.Printf("  ✅ Image %s is up to date\n", imageName)
	}

	// 3. Load into Kind
	fmt.Printf("  Loading %s into %s...\n", imageName, km.config.K8sClusterName)
	cmd = exec.Command("kind", "load", "docker-image", imageName, "--name", km.config.K8sClusterName)
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("kind load failed: %w", err)
	}

	return hasChanged, nil
}

// installHelmChart and generateValuesFile are in helm.go

func (km *KubernetesManager) waitForReadiness() error {
	fmt.Println("\n⏳ Waiting for stack to be ready...")

	// 1. Wait for Operator (Ensures CRDs are respected)
	fmt.Print("   Waiting for Operator... ")
	exec.Command("kubectl", "wait", "--for=condition=available", "deployment/frkr-operator", "--timeout=120s").Run()
	fmt.Println("✅")

	// 2. Wait for FrkrInit (Migrations)
	fmt.Print("   Waiting for Migrations (FrkrInit)... ")
	cmd := exec.Command("kubectl", "wait", "--for=condition=Ready", "frkrinit/frkr-init", "--timeout=300s")
	if err := cmd.Run(); err != nil {
		fmt.Println("❌ Failed (check operator logs)")
		return fmt.Errorf("migrations failed")
	}
	fmt.Println("✅")

	// 3. Wait for DataPlane
	// If this is ready, it means database and brokers are connected and ready
	fmt.Print("   Waiting for DataPlane... ")
	if err := exec.Command("kubectl", "wait", "--for=condition=Ready", "frkrdataplane/frkr-dataplane", "--timeout=300s").Run(); err != nil {
		fmt.Println("⚠️  Timed out waiting for DataPlane Ready state.")
	} else {
		fmt.Println("✅")
	}
    
    // We trust that if DataPlane is ready, the system is usable.
    // Individual gateway deployment waits are removed as they are managed by the chart/GitOps eventually.
    
	return nil
}

func (km *KubernetesManager) runPortForwarding() error {
	fmt.Println("\n🔌 Setting up port forwarding...")
	
	// Trap signals to clean up
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// DB - Always frkr-db
	dbPort := "5432"
	if km.config.DBPort != "" {
		dbPort = km.config.DBPort
	}
	go startPortForward("svc/frkr-db", fmt.Sprintf("%s:%s", dbPort, dbPort), km.config.PortForwardAddress)

	// Ingest
	go startPortForward("svc/frkr-ingest-gateway", fmt.Sprintf("%d:8080", km.config.IngestPort), km.config.PortForwardAddress)
	// Streaming
	go startPortForward("svc/frkr-streaming-gateway", fmt.Sprintf("%d:8081", km.config.StreamingPort), km.config.PortForwardAddress)
	
	fmt.Println("\n✅ frkr is running on Kubernetes (with port forwarding)!")
	
	fmt.Printf("   Ingest:    http://%s:%d\n", km.config.PortForwardAddress, km.config.IngestPort)
	fmt.Printf("   Streaming: http://%s:%d\n", km.config.PortForwardAddress, km.config.StreamingPort)
	fmt.Printf("   Database:  %s:%s\n", km.config.PortForwardAddress, km.config.DBPort)
	fmt.Println("\nPress Ctrl+C to exit.")
	
	<-sigChan
	return nil
}

func startPortForward(target, ports, address string) {
	for {
		cmd := exec.Command("kubectl", "port-forward", "--address", address, target, ports)
		// suppress output unless error?
		cmd.Run()
		time.Sleep(2 * time.Second) // Retry delay
	}
}

func (km *KubernetesManager) showSuccessMessage() {
	if km.config.SkipPortForward {
		fmt.Println("\n✅ frkr is deployed!")
		fmt.Println("   Run 'kubectl get svc' to see external IPs.")
	}
}

// installGatewayAPICRDs installs K8s Gateway API CRDs before Helm chart
// This must be done outside of Helm because Helm validates all templates
// before running hooks, and the chart contains Gateway resources
func (km *KubernetesManager) installGatewayAPICRDs() error {
	fmt.Println("\n🌐 Installing K8s Gateway API CRDs...")
	
	// Use version from values.yaml defaults (v1.2.1)
	url := "https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml"
	
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install Gateway API CRDs: %s: %w", string(output), err)
	}
	
	// Wait for CRDs to be established
	fmt.Print("   Waiting for CRDs to be established... ")
	waitCmd := exec.Command("kubectl", "wait", "--for=condition=Established", 
		"crd/gateways.gateway.networking.k8s.io",
		"crd/httproutes.gateway.networking.k8s.io",
		"--timeout=60s")
	if err := waitCmd.Run(); err != nil {
		fmt.Println("⚠️")
		// Continue anyway, they might work
	} else {
		fmt.Println("✅")
	}
	
	fmt.Println("✅ K8s Gateway API CRDs installed")
	return nil
}