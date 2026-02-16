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

	// 3. Build and Load Images
	updatedImages := make(map[string]bool)
	if km.config.ImageLoadCommand != "" {
		var err error
		updatedImages, err = km.buildAndLoadImages()
		if err != nil {
			return err
		}
	} else if km.config.ImageRegistry != "" && km.config.Rebuild {
		// Cloud/Remote Cluster: Build and Push (Only if requested)
		var err error
		updatedImages, err = km.PushImages()
		if err != nil {
			return fmt.Errorf("failed to auto-push images: %w", err)
		}
	}

	// 4. Install K8s Gateway API CRDs (must be done BEFORE Helm and BEFORE Envoy Gateway)
	// kubectl apply is idempotent and uses client-side apply, so it never conflicts
	// with prior installs regardless of who created the CRDs.
	if err := km.installGatewayAPICRDs(); err != nil {
		return err
	}

	// 5. Install Envoy Gateway controller (if ingress mode)
	// Uses --skip-crds because CRDs are already installed above via kubectl.
	if km.config.ExternalAccess == "ingress" {
		if err := km.installEnvoyGateway(); err != nil {
			return err
		}
	}

	// 5b. Install cert-manager (if TLS is requested)
	// Installed as a separate Helm release so the controller is running
	// before ClusterIssuer/Certificate CRs are applied by the main chart.
	if km.config.InstallCertManager {
		if err := km.installCertManager(); err != nil {
			return err
		}
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
	ctxCmd := exec.Command("kubectl", "config", "current-context")
	ctxOutput, err := ctxCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current kubernetes context: %w", err)
	}
	currentCtx := strings.TrimSpace(string(ctxOutput))

	if km.config.K8sClusterName != "" {
		if currentCtx != km.config.K8sClusterName {
			return fmt.Errorf("context mismatch!\n   Active Context: %s\n   Configured Cluster: %s\n\n👉 Please switch your kubectl context:\n   kubectl config use-context %s",
				currentCtx, km.config.K8sClusterName, km.config.K8sClusterName)
		}
		return nil
	}

	km.config.K8sClusterName = currentCtx
	return nil
}

func (km *KubernetesManager) buildAndLoadImages() (map[string]bool, error) {
	fmt.Println("\n📦 Building and loading images into cluster...")
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
		upd, err := km.buildAndLoadImage(p, "frkr-operator:0.1.1")
		if err != nil { return nil, err }
		updated["frkr-operator"] = upd
	}

	// Pre-load infrastructure images so the cluster doesn't pull them at deploy time.
	// frkrup always deploys with values-full.yaml which provisions postgres and redpanda,
	// so we always need these regardless of the db_host/broker_host config values.
	infraImages := []string{
		"busybox",
		"postgres:15-alpine",
		"docker.redpanda.com/redpandadata/redpanda:latest",
	}
	if err := km.pullAndLoadImages(infraImages); err != nil {
		return nil, err
	}

	return updated, nil
}

func (km *KubernetesManager) buildAndLoadImage(path, imageName string) (bool, error) {
	oldIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	oldIDBytes, _ := oldIDCmd.Output()
	oldID := strings.TrimSpace(string(oldIDBytes))

	fmt.Printf("  Building %s...\n", imageName)
	dockerfile := filepath.Join(path, "Dockerfile")
	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfile, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("build failed: %w", err)
	}

	newIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	newIDBytes, _ := newIDCmd.Output()
	newID := strings.TrimSpace(string(newIDBytes))

	hasChanged := oldID != newID
	if !hasChanged {
		fmt.Printf("  ✅ Image %s is up to date\n", imageName)
	}

	fmt.Printf("  Loading %s into cluster...\n", imageName)
	loadCmd := exec.Command("sh", "-c", km.config.ImageLoadCommand+" '"+imageName+"'")
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		return false, fmt.Errorf("image load failed (command: %s): %w", km.config.ImageLoadCommand, err)
	}

	return hasChanged, nil
}

// pullAndLoadImages pulls pre-built images from a registry and loads them into
// the cluster using the configured image_load_command. This ensures infrastructure
// images (postgres, redpanda, busybox) are available without in-cluster pulls.
func (km *KubernetesManager) pullAndLoadImages(images []string) error {
	if len(images) == 0 {
		return nil
	}
	fmt.Println("  Pre-loading infrastructure images...")
	for _, img := range images {
		fmt.Printf("    Pulling %s...\n", img)
		pullCmd := exec.Command("docker", "pull", img)
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if err := pullCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull %s: %w", img, err)
		}

		fmt.Printf("    Loading %s into cluster...\n", img)
		loadCmd := exec.Command("sh", "-c", km.config.ImageLoadCommand+" '"+img+"'")
		loadCmd.Stdout = os.Stdout
		loadCmd.Stderr = os.Stderr
		if err := loadCmd.Run(); err != nil {
			return fmt.Errorf("failed to load %s (command: %s): %w", img, km.config.ImageLoadCommand, err)
		}
	}
	return nil
}

// PushImages builds images and pushes them to the configured registry
func (km *KubernetesManager) PushImages() (map[string]bool, error) {
	registry := km.config.ImageRegistry
	if registry == "" {
		return nil, fmt.Errorf("property 'image_registry' is missing in config")
	}

	fmt.Printf("\n📦 Building and Pushing images to %s...\n", registry)
	updated := make(map[string]bool)

	images := []struct {
		Name    string
		Tag     string
		RepoKey string // helper for findGatewayRepoPath
	}{
		{"frkr-ingest-gateway", "0.1.0", "ingest"},
		{"frkr-streaming-gateway", "0.1.0", "streaming"},
		{"frkr-operator", "0.1.1", "operator"},
	}

	for _, img := range images {
		path, err := findGatewayRepoPath(img.RepoKey)
		if err != nil {
			return nil, fmt.Errorf("failed to find repo for %s: %w", img.Name, err)
		}

		fullImage := fmt.Sprintf("%s/%s:%s", registry, img.Name, img.Tag)
		fmt.Printf("  🚀 Processing %s...\n", fullImage)

		// 1. Build
		dockerfile := filepath.Join(path, "Dockerfile")
		buildCmd := exec.Command("docker", "build", "-t", fullImage, "-f", dockerfile, path)
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return nil, fmt.Errorf("build failed for %s: %w", img.Name, err)
		}

		// 2. Push
		pushCmd := exec.Command("docker", "push", fullImage)
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return nil, fmt.Errorf("push failed for %s: %w", img.Name, err)
		}
		fmt.Printf("  ✅ Pushed %s\n", fullImage)
		updated[img.Name] = true
	}

	fmt.Println("\n✅ All images pushed successfully!")
	return updated, nil
}

// installHelmChart and generateValuesFile are in helm.go

func (km *KubernetesManager) waitForReadiness() error {
	fmt.Println("\n⏳ Waiting for stack to be ready...")

	// 1. Wait for Operator
	fmt.Print("   Waiting for Operator... ")
	if err := km.retryKubectlWait(120*time.Second,
		"--for=condition=available", "deployment/frkr-operator", "--timeout=60s"); err != nil {
		fmt.Println("❌")
		return fmt.Errorf("operator not ready: %w", err)
	}
	fmt.Println("✅")

	// 2. Wait for FrkrInit (Migrations)
	// The FrkrInit CR is created by the operator after it starts, so it may
	// not exist immediately. retryKubectlWait handles the not-found race.
	fmt.Print("   Waiting for Migrations (FrkrInit)... ")
	if err := km.retryKubectlWait(120*time.Second,
		"--for=condition=Ready", "frkrinit/frkr-init", "--timeout=60s"); err != nil {
		fmt.Println("❌ Failed (check operator logs)")
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println("✅")

	// 3. Wait for DataPlane
	fmt.Print("   Waiting for DataPlane... ")
	if err := km.retryKubectlWait(120*time.Second,
		"--for=condition=Ready", "frkrdataplane/frkr-dataplane", "--timeout=60s"); err != nil {
		fmt.Println("⚠️  Timed out waiting for DataPlane Ready state.")
	} else {
		fmt.Println("✅")
	}

	return nil
}

// retryKubectlWait runs "kubectl wait" in a retry loop. kubectl wait exits
// immediately with an error when the target resource doesn't exist yet
// ("not found" / "no matching resources"). This loop re-issues the command
// until it succeeds or the deadline expires, handling the race between
// resource creation and readiness polling.
func (km *KubernetesManager) retryKubectlWait(deadline time.Duration, args ...string) error {
	end := time.Now().Add(deadline)
	for {
		cmd := exec.Command("kubectl", append([]string{"wait"}, args...)...)
		if cmd.Run() == nil {
			return nil
		}
		if time.Now().After(end) {
			return fmt.Errorf("timed out after %v waiting for: kubectl wait %s",
				deadline, strings.Join(args, " "))
		}
		time.Sleep(3 * time.Second)
	}
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
		if km.config.ExternalAccess == "ingress" {
			fmt.Println("   Envoy Gateway is routing external traffic.")
			fmt.Println("   Run 'kubectl get svc -n envoy-gateway-system' to see the external IP.")
		} else {
			fmt.Println("   Services are ClusterIP only (no external access configured).")
			fmt.Println("   Use 'kubectl port-forward' for local access.")
		}
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
		"crd/grpcroutes.gateway.networking.k8s.io",
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

// installEnvoyGateway installs the Envoy Gateway controller via Helm.
// The controller watches Gateway API resources (Gateway, HTTPRoute, GRPCRoute)
// and creates Envoy proxy pods + LoadBalancer services to serve traffic.
// Without it, the Gateway resources created by the frkr Helm chart are inert.
//
// Uses --skip-crds because installGatewayAPICRDs() has already installed them
// via kubectl apply. This avoids Helm SSA conflicts with kubectl-owned CRDs.
func (km *KubernetesManager) installEnvoyGateway() error {
	fmt.Println("\n🚪 Installing Envoy Gateway controller...")

	// Check if already installed via Helm
	check := exec.Command("helm", "status", "eg", "-n", "envoy-gateway-system")
	if check.Run() == nil {
		fmt.Println("✅ Envoy Gateway already installed")
		return nil
	}

	cmd := exec.Command("helm", "upgrade", "--install", "eg",
		"oci://docker.io/envoyproxy/gateway-helm",
		"--version", "v1.2.4",
		"-n", "envoy-gateway-system",
		"--create-namespace",
		"--skip-crds",
		"--wait",
		"--timeout", "120s")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install Envoy Gateway: %w", err)
	}

	fmt.Println("✅ Envoy Gateway controller installed")
	return nil
}

// installCertManager installs cert-manager via Helm as a separate release.
// Installed independently (not as a subchart) to avoid SSA conflicts with
// cloud admission enforcers (e.g. AKS admissionsenforcer) and to ensure
// the controller is running before ClusterIssuer/Certificate CRs are applied.
func (km *KubernetesManager) installCertManager() error {
	fmt.Println("\n🔐 Installing cert-manager...")

	// Fast path: if the release exists AND CRDs are present, skip.
	// We check CRDs explicitly because a failed migration (subchart → standalone)
	// can leave the release intact but CRDs deleted.
	releaseExists := exec.Command("helm", "status", "cert-manager", "-n", "cert-manager").Run() == nil
	crdsExist := exec.Command("kubectl", "get", "crd", "certificates.cert-manager.io").Run() == nil
	if releaseExists && crdsExist {
		fmt.Println("✅ cert-manager already installed")
		return nil
	}

	// Adopt any existing cert-manager CRDs that may be owned by a previous
	// Helm release (e.g. when cert-manager was a subchart of "frkr").
	km.adoptCertManagerCRDs()

	// Ensure the jetstack Helm repo is available (needed before installHelmChart
	// calls ensureHelmRepos).
	exec.Command("helm", "repo", "add", "jetstack", "https://charts.jetstack.io").Run()

	cmd := exec.Command("helm", "upgrade", "--install", "cert-manager",
		"jetstack/cert-manager",
		"--version", "v1.14.0",
		"-n", "cert-manager",
		"--create-namespace",
		"--set", "installCRDs=true",
		"--set", "featureGates=ExperimentalGatewayAPISupport=true",
		"--force",
		"--wait",
		"--timeout", "120s")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	fmt.Println("✅ cert-manager installed")
	return nil
}

// adoptCertManagerCRDs relabels any existing cert-manager CRDs so they are
// owned by the standalone "cert-manager" Helm release in the "cert-manager"
// namespace. This handles migration from the old subchart-based install where
// the CRDs were owned by the "frkr" release in "default".
// The resource-policy annotation prevents the subsequent "frkr" Helm upgrade
// from deleting the CRDs when it detects they are no longer in the chart.
func (km *KubernetesManager) adoptCertManagerCRDs() {
	crds := []string{
		"certificaterequests.cert-manager.io",
		"certificates.cert-manager.io",
		"challenges.acme.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"issuers.cert-manager.io",
		"orders.acme.cert-manager.io",
	}
	for _, crd := range crds {
		if exec.Command("kubectl", "get", "crd", crd).Run() != nil {
			continue
		}
		exec.Command("kubectl", "annotate", "crd", crd,
			"meta.helm.sh/release-name=cert-manager",
			"meta.helm.sh/release-namespace=cert-manager",
			"helm.sh/resource-policy=keep",
			"--overwrite").Run()
		exec.Command("kubectl", "label", "crd", crd,
			"app.kubernetes.io/managed-by=Helm",
			"--overwrite").Run()
	}
}