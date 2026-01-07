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

	// Build and load gateway images
	updatedImages, err := km.buildAndLoadImages()
	if err != nil {
		return err
	}

	// Install Gateway API CRDs (required for Envoy Gateway)
	if err := km.installGatewayAPICRDs(); err != nil {
		return err
	}

	// Install helm chart
	if err := km.installHelmChart(updatedImages); err != nil {
		return err
	}

	// Wait for migration job to complete (Helm hook runs migrations automatically)
	// But first, ensure the database exists from the host side via port-forward
	fmt.Println("\n🗄️  Ensuring database exists and running migrations...")
	if err := RunMigrationsK8s(km.config.MigrationsPath); err != nil {
		fmt.Printf("⚠️  Warning: Host-side migration failed: %v\n", err)
		fmt.Println("   Falling back to Helm migration job...")
	}

	if err := km.waitForMigrationJob(); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println("✅ Migrations completed")

	// Wait for pods
	if err := km.waitForPods(); err != nil {
		return err
	}

	// Create stream if requested
	if km.config.CreateStream {
		if err := km.createStreamAndTopic(); err != nil {
			fmt.Printf("⚠️  Stream provisioning failed: %v\n", err)
			// We don't fail the whole setup, but warn the user
		}
	}

	// Configure external access if requested
	if km.config.SkipPortForward && km.config.ExternalAccess != "none" {
		if err := km.configureExternalAccess(); err != nil {
			return fmt.Errorf("external access configuration failed: %w", err)
		}
	}

	// Setup port forwarding if requested (for local access)
	var portForwardCmds []*exec.Cmd
	if !km.config.SkipPortForward {
		var err error
		portForwardCmds, err = km.setupPortForwarding()
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

		// Verify gateways via port forward
		// For port-forwarding, gateways are accessible on localhost
		km.config.IngestHost = "localhost"
		km.config.StreamingHost = "localhost"
		
		fmt.Println("\n✅ Verifying gateways...")
		time.Sleep(2 * time.Second)
		gatewayMgr := NewGatewaysManager(km.config)
		if err := gatewayMgr.VerifyGateways(); err != nil {
			return fmt.Errorf("gateway verification failed: %w", err)
		}

		fmt.Println("\n✅ frkr is running on Kubernetes!")
		fmt.Printf("   Ingest Gateway: http://localhost:%d (via port-forward)\n", km.config.IngestPort)
		fmt.Printf("   Streaming Gateway: http://localhost:%d (via port-forward)\n", km.config.StreamingPort)
		fmt.Println("\nPress Ctrl+C to stop port forwarding and exit.")

		// Wait for interrupt
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
	} else {
		// Production mode - external access already configured if requested
		fmt.Println("\n✅ frkr is running on Kubernetes!")
		if km.config.ExternalAccess == "none" {
			km.showExternalAccessInfo()
		} else {
			// External access was configured, show the results
			km.showConfiguredExternalAccess()
			
			// Attempt to verify gateways if we have the host information
			if km.config.ExternalAccess == "loadbalancer" && 
			   km.config.IngestHost != "" && km.config.StreamingHost != "" {
				fmt.Println("\n🔍 Verifying gateways via LoadBalancer...")
				gatewayMgr := NewGatewaysManager(km.config)
				if err := gatewayMgr.VerifyGateways(); err != nil {
					fmt.Printf("   ⚠️  Gateway health check failed: %v\n", err)
					fmt.Println("   Gateways may still be starting - check status manually")
				} else {
					fmt.Println("   ✅ Gateways are healthy via LoadBalancer")
				}
			}
		}
		fmt.Println("\n✅ Setup complete! Gateways are ready for external traffic.")
		fmt.Println("   (Press Ctrl+C to exit)")
		// Wait for interrupt (but don't stop anything)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
	}

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

func (km *KubernetesManager) buildAndLoadImages() (map[string]bool, error) {
	fmt.Println("\n📦 Building and loading gateway images...")
	updated := make(map[string]bool)

	ingestGatewayPath, err := findGatewayRepoPath("ingest")
	if err != nil {
		return nil, fmt.Errorf("failed to find ingest gateway: %w", err)
	}
	upd, err := km.buildAndLoadImage(ingestGatewayPath, "frkr-ingest-gateway:0.1.0")
	if err != nil {
		return nil, fmt.Errorf("failed to build ingest gateway: %w", err)
	}
	updated["frkr-ingest-gateway"] = upd

	streamingGatewayPath, err := findGatewayRepoPath("streaming")
	if err != nil {
		return nil, fmt.Errorf("failed to find streaming gateway: %w", err)
	}
	upd, err = km.buildAndLoadImage(streamingGatewayPath, "frkr-streaming-gateway:0.1.0")
	if err != nil {
		return nil, fmt.Errorf("failed to build streaming gateway: %w", err)
	}
	updated["frkr-streaming-gateway"] = upd

	operatorPath, err := findGatewayRepoPath("operator")
	if err != nil {
		return nil, fmt.Errorf("failed to find operator: %w", err)
	}
	upd, err = km.buildAndLoadImage(operatorPath, "frkr-operator:0.1.1")
	if err != nil {
		return nil, fmt.Errorf("failed to build operator: %w", err)
	}
	updated["frkr-operator"] = upd

	return updated, nil
}

func (km *KubernetesManager) buildAndLoadImage(path, imageName string) (bool, error) {
	// 0. Get current image ID (if any)
	oldIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	oldIDBytes, _ := oldIDCmd.Output()
	oldID := strings.TrimSpace(string(oldIDBytes))

	// 1. Check for Dockerfile
	dockerfile := filepath.Join(path, "Dockerfile")
	if _, err := os.Stat(dockerfile); os.IsNotExist(err) {
		return false, fmt.Errorf("Dockerfile not found at %s", dockerfile)
	}

	// 2. Build image
	fmt.Printf("  Building %s...\n", imageName)
	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfile, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker build failed: %w", err)
	}

	// 3. Get new image ID
	newIDCmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageName)
	newIDBytes, err := newIDCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to inspect new image: %w", err)
	}
	newID := strings.TrimSpace(string(newIDBytes))

	hasChanged := oldID != newID
	if !hasChanged {
		fmt.Printf("  ✅ Image %s is up to date (no change detected)\n", imageName)
		// We still load it into Kind just in case the cluster was recreated
	}

	// 4. Load into kind cluster
	fmt.Printf("  Loading %s into kind cluster '%s'...\n", imageName, km.config.K8sClusterName)
	cmd = exec.Command("kind", "load", "docker-image", imageName, "--name", km.config.K8sClusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("kind load failed (make sure kind cluster exists): %w", err)
	}

	return hasChanged, nil
}

// installGatewayAPICRDs installs the Kubernetes Gateway API CRDs required for Envoy Gateway
func (km *KubernetesManager) installGatewayAPICRDs() error {
	fmt.Println("\n📦 Installing Gateway API CRDs...")
	
	// Check if CRDs are already installed
	checkCmd := exec.Command("kubectl", "get", "crd", "gateways.gateway.networking.k8s.io")
	if err := checkCmd.Run(); err == nil {
		fmt.Println("✅ Gateway API CRDs already installed")
		return nil
	}

	// Install Gateway API CRDs from official release
	const gatewayAPIVersion = "v1.1.0"
	crdURL := fmt.Sprintf("https://github.com/kubernetes-sigs/gateway-api/releases/download/%s/standard-install.yaml", gatewayAPIVersion)
	
	cmd := exec.Command("kubectl", "apply", "-f", crdURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install Gateway API CRDs: %w", err)
	}
	
	fmt.Println("✅ Gateway API CRDs installed")
	return nil
}

func (km *KubernetesManager) installHelmChart(updatedImages map[string]bool) error {
	helmPath, err := findInfraRepoPath("helm")
	if err != nil {
		return fmt.Errorf("failed to find frkr-infra-helm: %w", err)
	}

	// Check if release already exists
	checkCmd := exec.Command("helm", "list", "-q", "-f", "^frkr$")
	checkCmd.Dir = helmPath
	output, err := checkCmd.Output()
	releaseExists := err == nil && strings.TrimSpace(string(output)) == "frkr"

	if releaseExists {
		fmt.Println("\n📥 Upgrading existing frkr Helm chart...")
	} else {
		fmt.Println("\n📥 Installing frkr Helm chart...")
	}

	// Use upgrade --install to handle both install and upgrade cases
	helmCmd := exec.Command("helm", "upgrade", "--install", "frkr", ".", "-f", "values-full.yaml")
	helmCmd.Dir = helmPath
	helmCmd.Stdout = os.Stdout
	helmCmd.Stderr = os.Stderr
	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade/install failed: %w", err)
	}

	// Only restart deployments that actually have updated images
	if releaseExists {
		toRestart := []string{}
		for dep, changed := range updatedImages {
			if changed {
				toRestart = append(toRestart, dep)
			}
		}

		if len(toRestart) > 0 {
			fmt.Printf("\n🔄 Restarting %d deployments to use new images...\n", len(toRestart))
			if err := km.restartDeployments(toRestart); err != nil {
				fmt.Printf("⚠️  Warning: Failed to restart deployments: %v\n", err)
			}
		} else {
			fmt.Println("\n✅ All deployments are up to date (no images changed)")
		}
	}

	return nil
}

// restartDeployments restarts the specified deployments
func (km *KubernetesManager) restartDeployments(deployments []string) error {
	for _, deployment := range deployments {
		fmt.Printf("  Restarting %s...\n", deployment)
		cmd := exec.Command("kubectl", "rollout", "restart", "deployment", deployment)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restart %s: %w", deployment, err)
		}
	}

	// Wait for rollouts to complete
	fmt.Println("  Waiting for rollouts to complete...")
	for _, deployment := range deployments {
		cmd := exec.Command("kubectl", "rollout", "status", "deployment", deployment, "--timeout=120s")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("rollout for %s failed or timed out: %w", deployment, err)
		}
	}

	return nil
}

// waitForMigrationJob waits for the Helm migration job to complete
func (km *KubernetesManager) waitForMigrationJob() error {
	// The migration job name follows the pattern: frkr-migrations
	// Check if job exists and wait for it to complete
	cmd := exec.Command("kubectl", "get", "job", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={.items[?(@.metadata.name==\"frkr-migrations\")].metadata.name}")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		// Job might not exist yet or already completed (deleted by hook-delete-policy)
		// Check if there are any migration jobs
		cmd = exec.Command("kubectl", "get", "job", "-l", "app.kubernetes.io/name=frkr", "-o", "name")
		output, err = cmd.Output()
		if err != nil || strings.TrimSpace(string(output)) == "" {
			// No migration job found - might have already completed and been deleted
			// This is fine, migrations run as Helm hooks and may complete quickly
			fmt.Println("   Migration job not found (may have already completed)")
			return nil
		}
	}

	// Wait for the migration job to complete
	jobName := "frkr-migrations"
	fmt.Printf("   Waiting for migration job '%s' to complete...\n", jobName)
	cmd = exec.Command("kubectl", "wait", "--for=condition=complete", "job", jobName, "--timeout=300s")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Check if job failed
		cmd = exec.Command("kubectl", "get", "job", jobName, "-o", "jsonpath={.status.conditions[?(@.type==\"Failed\")].status}")
		failedOutput, _ := cmd.Output()
		if strings.TrimSpace(string(failedOutput)) == "True" {
			// Get job logs for debugging
			fmt.Println("\n❌ Migration job failed. Checking logs...")
			cmd = exec.Command("kubectl", "logs", "-l", "job-name="+jobName, "--tail=50")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			return fmt.Errorf("migration job failed")
		}
		// Job might have been deleted already (hook-delete-policy)
		fmt.Println("   Migration job may have already completed")
	}
	return nil
}

func (km *KubernetesManager) waitForPods() error {
	fmt.Println("\n⏳ Waiting for gateway deployments to be ready...")

	// Wait for all required deployments in a single command for better efficiency
	// This allows kubectl to monitor them in parallel
	required := []string{
		"deployment/frkr-ingest-gateway",
		"deployment/frkr-streaming-gateway",
	}

	args := append([]string{"wait", "--for=condition=available", "--timeout=300s"}, required...)
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("required gateways failed to become ready: %w", err)
	}

	// For the operator, we only wait if it actually exists (to avoid unnecessary timeout if disabled)
	checkCmd := exec.Command("kubectl", "get", "deployment", "frkr-operator")
	if err := checkCmd.Run(); err == nil {
		fmt.Print("⏳ Waiting for optional operator to be ready... ")
		// Use a much shorter timeout for optional components
		cmd = exec.Command("kubectl", "wait", "--for=condition=available", "deployment/frkr-operator", "--timeout=5s")
		if err := cmd.Run(); err != nil {
			fmt.Println("⚠️  timed out (not ready yet)")
		} else {
			fmt.Println("✅ ready")
		}
	} else {
		fmt.Println("ℹ️  Optional operator deployment not found, skipping wait")
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

// showExternalAccessInfo displays information about how to access gateways externally
func (km *KubernetesManager) showExternalAccessInfo() {
	fmt.Println("\n📡 External Access Information:")
	fmt.Println("")

	// Check current service types and external IPs
	cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.spec.type}{\"\\t\"}{.status.loadBalancer.ingress[0].ip}{\"\\n\"}{end}")
	output, err := cmd.Output()

	hasLoadBalancer := false
	if err == nil && len(output) > 0 {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == "LoadBalancer" {
				hasLoadBalancer = true
				serviceName := parts[0]
				externalIP := "<pending>"
				if len(parts) >= 3 && parts[2] != "" {
					externalIP = parts[2]
				}
				if strings.Contains(serviceName, "ingest") {
					fmt.Printf("   ✅ Ingest Gateway (LoadBalancer): %s:8080\n", externalIP)
				} else if strings.Contains(serviceName, "streaming") {
					fmt.Printf("   ✅ Streaming Gateway (LoadBalancer): %s:8081\n", externalIP)
				}
			}
		}
	}

	if !hasLoadBalancer {
		fmt.Println("   The gateways are currently exposed via ClusterIP services (internal only).")
		fmt.Println("   To enable external access, you have several options:")
		fmt.Println("")
		fmt.Println("   1. LoadBalancer Service (Cloud Providers):")
		fmt.Println("      Update the service type in the Helm chart:")
		fmt.Println("      - frkr-infra-helm/templates/ingest-gateway/service.yaml")
		fmt.Println("      - frkr-infra-helm/templates/streaming-gateway/service.yaml")
		fmt.Println("      Change 'type: ClusterIP' to 'type: LoadBalancer'")
		fmt.Println("      Then run: helm upgrade frkr <helm-path>")
		fmt.Println("")
		fmt.Println("   2. Ingress Controller:")
		fmt.Println("      Configure an Ingress resource pointing to:")
		fmt.Println("      - frkr-ingest-gateway:8080")
		fmt.Println("      - frkr-streaming-gateway:8081")
		fmt.Println("")
		fmt.Println("   3. NodePort (for testing):")
		fmt.Println("      Change service type to 'NodePort' and access via <node-ip>:<node-port>")
		fmt.Println("")
	}

	fmt.Println("   Current service endpoints (cluster-internal):")
	fmt.Println("   - Ingest Gateway:    frkr-ingest-gateway:8080")
	fmt.Println("   - Streaming Gateway: frkr-streaming-gateway:8081")
	fmt.Println("")
	fmt.Println("   To check service status:")
	fmt.Println("   kubectl get svc -l app.kubernetes.io/name=frkr")
}

// showConfiguredExternalAccess shows information about configured external access
func (km *KubernetesManager) showConfiguredExternalAccess() {
	switch km.config.ExternalAccess {
	case "loadbalancer":
		fmt.Println("\n📡 LoadBalancer Services Configured:")
		fmt.Println("   Checking for external IPs...")
		cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.loadBalancer.ingress[0].ip}{\"\\n\"}{end}")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					svcName := parts[0]
					externalIP := parts[1]
					if externalIP != "" && externalIP != "<pending>" {
						if strings.Contains(svcName, "ingest") {
							fmt.Printf("   ✅ Ingest Gateway:    http://%s:8080\n", externalIP)
						} else if strings.Contains(svcName, "streaming") {
							fmt.Printf("   ✅ Streaming Gateway: http://%s:8081\n", externalIP)
						}
					} else {
						if strings.Contains(svcName, "ingest") {
							fmt.Println("   ⏳ Ingest Gateway:    Waiting for LoadBalancer IP...")
						} else if strings.Contains(svcName, "streaming") {
							fmt.Println("   ⏳ Streaming Gateway: Waiting for LoadBalancer IP...")
						}
					}
				}
			}
		}
		fmt.Println("\n   Check status with: kubectl get svc -l app.kubernetes.io/name=frkr")
	case "ingress":
		fmt.Printf("\n📡 Ingress Configured:\n")
		fmt.Printf("   Host: %s\n", km.config.IngressHost)
		fmt.Printf("   Ingest Gateway:    http://%s/ingest\n", km.config.IngressHost)
		fmt.Printf("   Streaming Gateway: http://%s/streaming\n", km.config.IngressHost)
		fmt.Println("\n   Check status with: kubectl get ingress frkr-gateways")
	}
}

// configureExternalAccess configures LoadBalancer or Ingress based on user choice
func (km *KubernetesManager) configureExternalAccess() error {
	switch km.config.ExternalAccess {
	case "loadbalancer":
		return km.configureLoadBalancer()
	case "ingress":
		return km.configureIngress()
	default:
		return nil // "none" - no configuration needed
	}
}

// configureLoadBalancer changes service types from ClusterIP to LoadBalancer
func (km *KubernetesManager) configureLoadBalancer() error {
	fmt.Println("\n🔧 Configuring LoadBalancer services...")

	services := []string{
		"frkr-ingest-gateway",
		"frkr-streaming-gateway",
	}

	for _, svcName := range services {
		fmt.Printf("   Patching service %s to LoadBalancer...\n", svcName)
		cmd := exec.Command("kubectl", "patch", "service", svcName, "-p", `{"spec":{"type":"LoadBalancer"}}`)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to patch service %s: %w", svcName, err)
		}
		fmt.Printf("   ✅ Service %s configured as LoadBalancer\n", svcName)
	}

	fmt.Println("\n⏳ Waiting for LoadBalancer IPs to be assigned...")
	fmt.Println("   (This may take a few minutes depending on your cloud provider)")

	// Wait for LoadBalancer IPs with timeout
	maxWait := 300 // 5 minutes
	for i := 0; i < maxWait; i++ {
		time.Sleep(2 * time.Second)

		// Check if both services have external IPs
		cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.loadBalancer.ingress[0].ip}{\"\\n\"}{end}")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			allReady := true
			for _, line := range lines {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					svcName := parts[0]
					externalIP := parts[1]
					if externalIP == "" || externalIP == "<pending>" {
						allReady = false
						break
					}
					if strings.Contains(svcName, "ingest") {
						fmt.Printf("   ✅ Ingest Gateway LoadBalancer IP: %s\n", externalIP)
						// Set gateway host for health checks
						km.config.IngestHost = externalIP
					} else if strings.Contains(svcName, "streaming") {
						fmt.Printf("   ✅ Streaming Gateway LoadBalancer IP: %s\n", externalIP)
						// Set gateway host for health checks
						km.config.StreamingHost = externalIP
					}
				}
			}
			if allReady && len(lines) >= 2 {
				fmt.Println("\n✅ LoadBalancer IPs assigned successfully!")
				// Verify gateways are accessible via LoadBalancer
				fmt.Println("\n🔍 Verifying gateways via LoadBalancer...")
				gatewayMgr := NewGatewaysManager(km.config)
				if err := gatewayMgr.VerifyGateways(); err != nil {
					fmt.Printf("   ⚠️  Gateway health check failed: %v\n", err)
					fmt.Println("   Gateways may still be starting - check status manually")
				} else {
					fmt.Println("   ✅ Gateways are healthy via LoadBalancer")
				}
				return nil
			}
		}

		if (i+1)%30 == 0 {
			fmt.Printf("   Still waiting... (%d/%d seconds)\n", i+1, maxWait)
		}
	}

	fmt.Println("\n⚠️  LoadBalancer IPs not assigned within timeout")
	fmt.Println("   Services are configured as LoadBalancer - check status with:")
	fmt.Println("   kubectl get svc -l app.kubernetes.io/name=frkr")
	return nil // Don't fail - services are configured, just waiting for IPs
}

// configureIngress creates Ingress resources for the gateways
func (km *KubernetesManager) configureIngress() error {
	fmt.Println("\n🔧 Configuring Ingress...")

	// Check if an Ingress controller is available and get ingressClassName
	fmt.Println("   Checking for Ingress controller...")
	cmd := exec.Command("kubectl", "get", "ingressclass", "-o", "jsonpath={.items[0].metadata.name}")
	output, err := cmd.Output()
	ingressClassName := ""
	if err == nil && len(output) > 0 {
		ingressClassName = strings.TrimSpace(string(output))
		fmt.Printf("   ✅ Ingress controller detected: %s\n", ingressClassName)
	} else {
		fmt.Println("   ⚠️  No IngressClass found. You may need to install an Ingress controller.")
		fmt.Println("      Common options: nginx-ingress, traefik, or cloud-specific controllers")
		fmt.Print("   Continue anyway? (yes/no) [yes]: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "no" || answer == "n" {
			return fmt.Errorf("ingress setup cancelled")
		}
		// Ask for ingressClassName manually
		fmt.Print("   IngressClass name (optional, press Enter to skip): ")
		scanner.Scan()
		ingressClassName = strings.TrimSpace(scanner.Text())
	}

	// Create Ingress resource
	ingressClassNameField := ""
	if ingressClassName != "" {
		ingressClassNameField = fmt.Sprintf("  ingressClassName: %s\n", ingressClassName)
	}

	ingressTLSField := ""
	if km.config.IngressTLSSecret != "" {
		ingressTLSField = fmt.Sprintf(`  tls:
  - hosts:
    - %s
    secretName: %s
`, km.config.IngressHost, km.config.IngressTLSSecret)
	}

	ingressYAML := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: frkr-gateways
  labels:
    app.kubernetes.io/name: frkr
spec:
%s  rules:
  - host: %s
    http:
      paths:
      - path: /ingest
        pathType: Prefix
        backend:
          service:
            name: frkr-ingest-gateway
            port:
              number: 8080
      - path: /streaming
        pathType: Prefix
        backend:
          service:
            name: frkr-streaming-gateway
            port:
              number: 8081
%s`, ingressClassNameField, ingressTLSField, km.config.IngressHost)

	fmt.Printf("   Creating Ingress resource for host: %s\n", km.config.IngressHost)
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(ingressYAML)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create Ingress resource: %w", err)
	}

	fmt.Println("   ✅ Ingress resource created")
	fmt.Println("\n⏳ Waiting for Ingress to be ready...")

	// Wait for Ingress to get an address (IP or hostname)
	maxWait := 120 // 2 minutes
	for i := 0; i < maxWait; i++ {
		time.Sleep(2 * time.Second)

		// Try IP first
		cmd := exec.Command("kubectl", "get", "ingress", "frkr-gateways", "-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
		output, err := cmd.Output()
		address := strings.TrimSpace(string(output))

		// If no IP, try hostname (some cloud providers use hostname)
		if address == "" {
			cmd = exec.Command("kubectl", "get", "ingress", "frkr-gateways", "-o", "jsonpath={.status.loadBalancer.ingress[0].hostname}")
			output, err = cmd.Output()
			address = strings.TrimSpace(string(output))
		}

		if err == nil && address != "" {
			fmt.Printf("   ✅ Ingress address: %s\n", address)
			fmt.Printf("\n📡 Gateway URLs:\n")
			fmt.Printf("   Ingest Gateway:    http://%s/ingest\n", km.config.IngressHost)
			fmt.Printf("   Streaming Gateway: http://%s/streaming\n", km.config.IngressHost)
			fmt.Println("\n   Note: Ensure DNS points to the Ingress address above")
			
			// For Ingress, we can't easily verify from here (would need DNS resolution)
			// But we can set the host for potential future verification
			// The health check URLs will use the Ingress hostname with /ingest/health and /streaming/health paths
			fmt.Println("\n   ⚠️  Gateway health verification skipped for Ingress")
			fmt.Println("   (DNS resolution required - verify manually after DNS is configured)")
			return nil
		}

		if (i+1)%20 == 0 {
			fmt.Printf("   Still waiting... (%d/%d seconds)\n", i+1, maxWait)
		}
	}

	fmt.Println("\n⚠️  Ingress address not assigned within timeout")
	fmt.Println("   Ingress resource created - check status with:")
	fmt.Println("   kubectl get ingress frkr-gateways")
	return nil // Don't fail - Ingress is created, just waiting for address
}

// createStreamAndTopic provisions the initial stream and topic in Kubernetes
func (km *KubernetesManager) createStreamAndTopic() error {
	fmt.Printf("\n📡 Provisioning stream '%s' in Kubernetes...\n", km.config.StreamName)

	// 1. Setup temporary port-forwarding for database and broker
	
	// DB Port forward
	fmt.Printf("   Setting up temporary port-forward for database (svc/frkr-cockroachdb:26257)...\n")
	dbCmd := exec.Command("kubectl", "port-forward", "svc/frkr-cockroachdb", fmt.Sprintf("%s:26257", km.config.DBPort))
	if err := dbCmd.Start(); err != nil {
		return fmt.Errorf("failed to start database port-forward: %w", err)
	}
	defer dbCmd.Process.Kill()

	// Broker Port forward
	fmt.Printf("   Setting up temporary port-forward for broker (svc/frkr-redpanda:9092)...\n")
	brokerCmd := exec.Command("kubectl", "port-forward", "svc/frkr-redpanda", fmt.Sprintf("%s:9092", km.config.BrokerPort))
	if err := brokerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start broker port-forward: %w", err)
	}
	defer brokerCmd.Process.Kill()

	// Wait for port forwards to be ready and verified
	time.Sleep(5 * time.Second)

	// 2. Build URLs
	// Use local ports established via port-forward
	oldDBHost := km.config.DBHost
	oldBrokerHost := km.config.BrokerHost
	km.config.DBHost = "localhost"
	km.config.BrokerHost = "localhost"
	defer func() {
		km.config.DBHost = oldDBHost
		km.config.BrokerHost = oldBrokerHost
	}()

	dbURL := km.config.BuildDBURL()
	brokerURL := km.config.BuildBrokerURL()

	// 3. Create stream in database
	dbMgr := NewDatabaseManager(dbURL)
	stream, err := dbMgr.CreateStream(km.config.StreamName)
	if err != nil {
		return fmt.Errorf("failed to create stream in database: %w", err)
	}
	fmt.Printf("   ✅ Stream '%s' created in database\n", km.config.StreamName)

	// 4. Create topic in broker
	brokerMgr := NewBrokerManager(brokerURL)
	if err := brokerMgr.CreateTopic(stream.Topic); err != nil {
		return fmt.Errorf("failed to create topic in broker: %w", err)
	}
	fmt.Printf("   ✅ Topic '%s' created in broker\n", stream.Topic)

	return nil
}
