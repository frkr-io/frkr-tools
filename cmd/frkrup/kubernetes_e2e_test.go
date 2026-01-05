//go:build e2e
// +build e2e

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testClusterName = "frkrup-e2e-test"
	testNamespace   = "default"
)

// setupKindCluster creates a kind cluster for testing
func setupKindCluster(t *testing.T) {
	t.Helper()

	// Check if cluster already exists
	cmd := exec.Command("kind", "get", "clusters")
	output, _ := cmd.Output()
	if strings.Contains(string(output), testClusterName) {
		t.Logf("Kind cluster %s already exists, reusing...", testClusterName)
		return
	}

	t.Logf("Creating kind cluster %s...", testClusterName)
	cmd = exec.Command("kind", "create", "cluster", "--name", testClusterName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's because cluster already exists
		if strings.Contains(string(output), "already exists") {
			t.Logf("Cluster %s already exists, reusing...", testClusterName)
			return
		}
		require.NoError(t, err, "Failed to create kind cluster: %s", string(output))
	}

	// Wait for cluster to be ready
	t.Log("Waiting for cluster to be ready...")
	cmd = exec.Command("kubectl", "cluster-info", "--context", fmt.Sprintf("kind-%s", testClusterName))
	for i := 0; i < 30; i++ {
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.NoError(t, cmd.Run(), "Cluster not ready after 60 seconds")
}

// teardownKindCluster deletes the kind cluster
func teardownKindCluster(t *testing.T) {
	t.Helper()
	t.Logf("Deleting kind cluster %s...", testClusterName)
	cmd := exec.Command("kind", "delete", "cluster", "--name", testClusterName)
	// Don't fail if cluster doesn't exist
	_ = cmd.Run()
}

// installMetalLB installs MetalLB for LoadBalancer support in kind
func installMetalLB(t *testing.T) {
	t.Helper()
	t.Log("Installing MetalLB for LoadBalancer support...")

	// Apply MetalLB manifest
	cmd := exec.Command("kubectl", "apply", "-f", "https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "Failed to install MetalLB")

	// Wait for MetalLB to be ready
	t.Log("Waiting for MetalLB to be ready...")
	cmd = exec.Command("kubectl", "wait", "--namespace", "metallb-system", "--for=condition=ready", "pod", "--selector", "app=metallb", "--timeout=90s")
	require.NoError(t, cmd.Run(), "MetalLB not ready")

	// Configure MetalLB IP pool (use Docker network range)
	ipPool := `apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-pool
  namespace: metallb-system
spec:
  addresses:
  - 172.18.255.200-172.18.255.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default
  namespace: metallb-system
spec:
  ipAddressPools:
  - default-pool
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(ipPool)
	require.NoError(t, cmd.Run(), "Failed to configure MetalLB IP pool")
}

// installIngressController installs nginx ingress controller
func installIngressController(t *testing.T) {
	t.Helper()
	t.Log("Installing nginx Ingress controller...")

	cmd := exec.Command("kubectl", "apply", "-f", "https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "Failed to install nginx Ingress controller")

	// Wait for Ingress controller to be ready
	t.Log("Waiting for Ingress controller to be ready...")
	cmd = exec.Command("kubectl", "wait", "--namespace", "ingress-nginx", "--for=condition=ready", "pod", "--selector", "app.kubernetes.io/component=controller", "--timeout=180s")
	require.NoError(t, cmd.Run(), "Ingress controller not ready")
}

// setupTestHelmChart installs the Helm chart for testing
func setupTestHelmChart(t *testing.T) {
	t.Helper()
	t.Log("Installing Helm chart...")

	// Get repo root (assume we're in cmd/frkrup)
	repoRoot, err := filepath.Abs("../../")
	require.NoError(t, err, "Failed to get repo root")

	helmPath := filepath.Join(repoRoot, "frkr-infra-helm")
	if _, err := os.Stat(helmPath); os.IsNotExist(err) {
		t.Fatalf("Helm chart not found at %s (submodule may not be initialized)", helmPath)
	}

	// Build and load gateway images first
	t.Log("Building and loading gateway images...")
	ingestPath := filepath.Join(repoRoot, "frkr-ingest-gateway")
	streamingPath := filepath.Join(repoRoot, "frkr-streaming-gateway")

	// Build images
	for _, path := range []struct{ path, name string }{
		{ingestPath, "frkr-ingest-gateway:0.1.0"},
		{streamingPath, "frkr-streaming-gateway:0.1.0"},
	} {
		if _, err := os.Stat(path.path); os.IsNotExist(err) {
			t.Fatalf("Gateway path not found: %s (submodule may not be initialized)", path.path)
		}
		cmd := exec.Command("docker", "build", "-t", path.name, path.path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "Failed to build image: %s", path.name)

		// Load into kind
		cmd = exec.Command("kind", "load", "docker-image", path.name, "--name", testClusterName)
		require.NoError(t, cmd.Run(), "Failed to load image: %s", path.name)
	}

	// Install Helm chart
	cmd := exec.Command("helm", "upgrade", "--install", "frkr", helmPath, "-f", filepath.Join(helmPath, "values-full.yaml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "Failed to install Helm chart")

	// Wait for pods to be ready
	t.Log("Waiting for pods to be ready...")
	cmd = exec.Command("kubectl", "wait", "--for=condition=ready", "pod", "-l", "app.kubernetes.io/name=frkr", "--timeout=300s")
	require.NoError(t, cmd.Run(), "Pods not ready")
}

// runMigrations runs database migrations
func runMigrations(t *testing.T) {
	t.Helper()
	t.Log("Running database migrations...")

	// Port forward database
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "port-forward", "svc/frkr-cockroachdb", "26257:26257")
	require.NoError(t, cmd.Start())
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	time.Sleep(2 * time.Second)

	// Run migrations using frkr-common migrate package directly
	repoRoot, err := filepath.Abs("../../")
	require.NoError(t, err)
	migrationsPath := filepath.Join(repoRoot, "..", "frkr-common", "migrations")
	if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
		// Try alternative path
		migrationsPath = filepath.Join(repoRoot, "frkr-common", "migrations")
	}

	dbURL := "postgres://root@localhost:26257/frkrdb?sslmode=disable"
	
	// Use frkrcfg as a subprocess for migrations
	frkrcfgPath := filepath.Join(repoRoot, "bin", "frkrcfg")
	if _, err := os.Stat(frkrcfgPath); os.IsNotExist(err) {
		// Build it
		buildCmd := exec.Command("go", "build", "-o", frkrcfgPath, "./cmd/frkrcfg")
		buildCmd.Dir = repoRoot
		require.NoError(t, buildCmd.Run(), "Failed to build frkrcfg")
	}

	cmd = exec.Command(frkrcfgPath, "migrate", "--db-url", dbURL, "--migrations-path", migrationsPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "Failed to run migrations")
}

// verifyGatewayHealth checks if a gateway is healthy via HTTP
func verifyGatewayHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: timeout}
	
	for i := 0; i < int(timeout.Seconds()/2); i++ {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	require.Fail(t, "Gateway health check failed", "Could not reach %s within %v", url, timeout)
}

func TestKubernetesExternalAccess_LoadBalancer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Check prerequisites
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	setupKindCluster(t)
	defer teardownKindCluster(t)

	installMetalLB(t)
	setupTestHelmChart(t)
	runMigrations(t)

	// Test LoadBalancer configuration
	t.Log("Testing LoadBalancer configuration...")

	// Patch services to LoadBalancer
	services := []string{"frkr-ingest-gateway", "frkr-streaming-gateway"}
	for _, svcName := range services {
		cmd := exec.Command("kubectl", "patch", "service", svcName, "-p", `{"spec":{"type":"LoadBalancer"}}`)
		require.NoError(t, cmd.Run(), "Failed to patch service: %s", svcName)
	}

	// Wait for LoadBalancer IPs (with timeout)
	t.Log("Waiting for LoadBalancer IPs...")
	maxWait := 120 // 2 minutes
	var ingestIP, streamingIP string
	for i := 0; i < maxWait; i++ {
		time.Sleep(2 * time.Second)
		cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.loadBalancer.ingress[0].ip}{\"\\n\"}{end}")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				parts := strings.Fields(line)
				if len(parts) >= 2 && parts[1] != "" && parts[1] != "<pending>" {
					if strings.Contains(parts[0], "ingest") {
						ingestIP = parts[1]
					} else if strings.Contains(parts[0], "streaming") {
						streamingIP = parts[1]
					}
				}
			}
			if ingestIP != "" && streamingIP != "" {
				break
			}
		}
		if (i+1)%20 == 0 {
			t.Logf("Still waiting for LoadBalancer IPs... (%d/%d seconds)", i+1, maxWait)
		}
	}

	require.NotEmpty(t, ingestIP, "Ingest gateway LoadBalancer IP not assigned")
	require.NotEmpty(t, streamingIP, "Streaming gateway LoadBalancer IP not assigned")

	// Verify gateways are accessible
	t.Logf("Verifying ingest gateway at http://%s:8080/health", ingestIP)
	verifyGatewayHealth(t, fmt.Sprintf("http://%s:8080/health", ingestIP), 30*time.Second)

	t.Logf("Verifying streaming gateway at http://%s:8081/health", streamingIP)
	verifyGatewayHealth(t, fmt.Sprintf("http://%s:8081/health", streamingIP), 30*time.Second)

	t.Log("✅ LoadBalancer test passed!")
}

func TestKubernetesExternalAccess_Ingress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Check prerequisites
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	setupKindCluster(t)
	defer teardownKindCluster(t)

	installIngressController(t)
	setupTestHelmChart(t)
	runMigrations(t)

	// Test Ingress configuration
	t.Log("Testing Ingress configuration...")

	// Get Ingress controller IP (for kind, it's usually the node IP)
	cmd := exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].status.addresses[?(@.type==\"InternalIP\")].address}")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to get node IP")
	nodeIP := strings.TrimSpace(string(output))
	require.NotEmpty(t, nodeIP, "Node IP not found")

	// Create Ingress resource
	testHost := "frkr-e2e-test.local"
	ingressYAML := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: frkr-gateways
  labels:
    app.kubernetes.io/name: frkr
spec:
  ingressClassName: nginx
  rules:
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
`, testHost)

	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(ingressYAML)
	require.NoError(t, cmd.Run(), "Failed to create Ingress resource")

	// Wait for Ingress to be ready
	t.Log("Waiting for Ingress to be ready...")
	maxWait := 120
	for i := 0; i < maxWait; i++ {
		time.Sleep(2 * time.Second)
		cmd := exec.Command("kubectl", "get", "ingress", "frkr-gateways", "-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
		output, err := cmd.Output()
		if err == nil {
			address := strings.TrimSpace(string(output))
			if address != "" {
				break
			}
		}
	}

	// Verify gateways are accessible via Ingress
	// For kind, we need to add /etc/hosts entry or use node IP directly
	// We'll use the node IP with Host header
	client := &http.Client{Timeout: 30 * time.Second}
	
	// Test ingest gateway
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/ingest/health", nodeIP), nil)
	require.NoError(t, err)
	req.Host = testHost
	
	t.Logf("Verifying ingest gateway via Ingress at http://%s/ingest/health (Host: %s)", nodeIP, testHost)
	for i := 0; i < 30; i++ {
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
		if i == 29 {
			require.Fail(t, "Ingest gateway not accessible via Ingress")
		}
	}

	// Test streaming gateway
	req, err = http.NewRequest("GET", fmt.Sprintf("http://%s/streaming/health", nodeIP), nil)
	require.NoError(t, err)
	req.Host = testHost
	
	t.Logf("Verifying streaming gateway via Ingress at http://%s/streaming/health (Host: %s)", nodeIP, testHost)
	for i := 0; i < 30; i++ {
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
		if i == 29 {
			require.Fail(t, "Streaming gateway not accessible via Ingress")
		}
	}

	t.Log("✅ Ingress test passed!")
}

func TestKubernetesExternalAccess_ClusterIP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Check prerequisites
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	setupKindCluster(t)
	defer teardownKindCluster(t)

	setupTestHelmChart(t)
	runMigrations(t)

	// Test ClusterIP (internal only) - verify services exist and are ClusterIP
	t.Log("Testing ClusterIP configuration...")

	// Verify services are ClusterIP
	cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.spec.type}{\"\\n\"}{end}")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to get services")

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "Expected at least 2 services")

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			svcName := parts[0]
			svcType := parts[1]
			require.Equal(t, "ClusterIP", svcType, "Service %s should be ClusterIP", svcName)
		}
	}

	// Verify services are accessible internally via port-forward
	t.Log("Verifying internal access via port-forward...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Port forward ingest gateway
	ingestPF := exec.CommandContext(ctx, "kubectl", "port-forward", "svc/frkr-ingest-gateway", "8080:8080")
	require.NoError(t, ingestPF.Start())
	defer ingestPF.Process.Kill()
	time.Sleep(2 * time.Second)

	verifyGatewayHealth(t, "http://localhost:8080/health", 10*time.Second)

	// Port forward streaming gateway
	streamingPF := exec.CommandContext(ctx, "kubectl", "port-forward", "svc/frkr-streaming-gateway", "8081:8081")
	require.NoError(t, streamingPF.Start())
	defer streamingPF.Process.Kill()
	time.Sleep(2 * time.Second)

	verifyGatewayHealth(t, "http://localhost:8081/health", 10*time.Second)

	t.Log("✅ ClusterIP test passed!")
}

