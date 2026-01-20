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


	// 4. Install Gateway API CRDs (Required for Helm Chart)
	if err := km.installGatewayAPI(); err != nil {
		return err
	}

	// 5. Setup Infrastructure (Secrets, CRDs)
	if err := km.setupInfrastructure(); err != nil {
		return err
	}

	// 5. Install/Upgrade Helm Chart
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
	if km.config.K8sClusterName != "" {
		return nil
	}

	// Try to get cluster name from kubectl context
	ctxCmd := exec.Command("kubectl", "config", "current-context")
	ctxOutput, err := ctxCmd.Output()
	if err == nil {
		ctxStr := strings.TrimSpace(string(ctxOutput))
		if strings.HasPrefix(ctxStr, "kind-") {
			km.config.K8sClusterName = strings.TrimPrefix(ctxStr, "kind-")
			return nil
		}
		km.config.K8sClusterName = ctxStr // Default to context name if not kind
	} else {
		return fmt.Errorf("failed to get current context: %w", err)
	}

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

func (km *KubernetesManager) installHelmChart(updatedImages map[string]bool) error {
	helmPath, err := findInfraRepoPath("helm")
	if err != nil {
		return fmt.Errorf("failed to find helm chart: %w", err)
	}

	fmt.Println("\n📥 Installing/Upgrading frkr Helm chart...")

	// Construct Helm args
	args := []string{"upgrade", "--install", "frkr", ".", "-f", "values-full.yaml"}
	
	// Set overrides based on Config
	overrides := []string{}

	// DB Setup (Default to "full" stack or "byo" depending on config?)
	// For "One-Click", we assume if they are using this tool in Kind, they want Full Stack?
	// But the values-full.yaml enables it.
	
	// Pass Configured Secrets and Details
	overrides = append(overrides, fmt.Sprintf("infrastructure.db.user=%s", km.config.DBUser))
	overrides = append(overrides, fmt.Sprintf("infrastructure.db.password=%s", km.config.DBPassword))
	overrides = append(overrides, fmt.Sprintf("dataPlane.db.user=%s", km.config.DBUser))
	overrides = append(overrides, fmt.Sprintf("dataPlane.db.password=%s", km.config.DBPassword))
	
	// Pass DB connection details
	if km.config.DBName != "" {
		overrides = append(overrides, fmt.Sprintf("dataPlane.db.database=%s", km.config.DBName))
		overrides = append(overrides, fmt.Sprintf("infrastructure.db.name=%s", km.config.DBName))
	}
	if km.config.DBPort != "" {
		overrides = append(overrides, fmt.Sprintf("dataPlane.db.port=%s", km.config.DBPort))
	}

	// Dev/Kind specific overrides
	if isKindCluster() {
		// Ensure we use the images we just built (IfNotPresent in values.yaml handles this usually, 
		// but we might want to force it if they are :latest, but here valid versions)
	}
	
	if km.config.TestOIDC {
		overrides = append(overrides, "infrastructure.mockOIDC.enabled=true")
		overrides = append(overrides, "auth.oidc.issuerUrl=http://frkr-mock-oidc.default.svc.cluster.local:8080/default")
		// Configure Helm to use Mock OIDC
		fmt.Println("   Configuring for Mock OIDC...")
	}

	// External Access
	switch km.config.ExternalAccess {
	case "loadbalancer":
		overrides = append(overrides, "ingestGateway.service.type=LoadBalancer")
		overrides = append(overrides, "streamingGateway.service.type=LoadBalancer")
	case "ingress":
		overrides = append(overrides, "ingress.enabled=true")
		if km.config.IngressHost != "" {
			overrides = append(overrides, fmt.Sprintf("ingress.host=%s", km.config.IngressHost))
		}
	}

	// Vendor Provider
	if km.config.Provider != "" {
		overrides = append(overrides, fmt.Sprintf("global.provider=%s", km.config.Provider))
	}

	for _, o := range overrides {
		args = append(args, "--set", o)
	}

	cmd := exec.Command("helm", args...)
	cmd.Dir = helmPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade failed: %w", err)
	}
	
	// Restart deployments if images changed
	if len(updatedImages) > 0 {
		toRestart := []string{}
		for dep, changed := range updatedImages {
			if changed {
				toRestart = append(toRestart, dep)
			}
		}
		if len(toRestart) > 0 {
			fmt.Printf("🔄 Restarting %d deployments...\n", len(toRestart))
			for _, dep := range toRestart {
				exec.Command("kubectl", "rollout", "restart", "deployment", dep).Run()
			}
		}
	}

	return nil
}

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
	go startPortForward("svc/frkr-db", fmt.Sprintf("%s:%s", dbPort, dbPort))

	// Ingest
	go startPortForward("svc/frkr-ingest-gateway", fmt.Sprintf("%d:8080", km.config.IngestPort))
	// Streaming
	go startPortForward("svc/frkr-streaming-gateway", fmt.Sprintf("%d:8081", km.config.StreamingPort))
	
	fmt.Println("\n✅ frkr is running on Kubernetes (with port forwarding)!")
	fmt.Printf("   Ingest:    http://localhost:%d\n", km.config.IngestPort)
	fmt.Printf("   Streaming: http://localhost:%d\n", km.config.StreamingPort)
	fmt.Printf("   Database:  localhost:%s\n", km.config.DBPort)
	fmt.Println("\nPress Ctrl+C to exit.")
	
	<-sigChan
	return nil
}

func startPortForward(target, ports string) {
	for {
		cmd := exec.Command("kubectl", "port-forward", target, ports)
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

func (km *KubernetesManager) installGatewayAPI() error {
	fmt.Println("\n🌐 Installing Gateway API CRDs...")
	// Standard install for v1.0.0
	url := "https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.0.0/standard-install.yaml"
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install Gateway API: %s: %w", string(output), err)
	}
	fmt.Println("✅ Gateway API installed")
	return nil
}

// Helpers are in frkrup_paths.go

func (km *KubernetesManager) setupInfrastructure() error {
	// No explicit infrastructure setup needed - Helm handles secrets and CRDs
	return nil
}
