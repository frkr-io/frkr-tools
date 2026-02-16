//go:build e2e
// +build e2e

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	testClusterName = "frkrup-e2e-test"
	testNamespace   = "default"
)

// ---------------------------------------------------------------------------
// Test-only infrastructure helpers (Kind, MetalLB, diagnostics, assertions)
// ---------------------------------------------------------------------------

// setupKindCluster creates a fresh Kind cluster for testing.
// Any leftover cluster from a previous (possibly crashed) run is deleted first.
func setupKindCluster(t *testing.T) {
	t.Helper()
	kindCtx := fmt.Sprintf("kind-%s", testClusterName)

	cmd := exec.Command("kind", "get", "clusters")
	output, _ := cmd.Output()
	if strings.Contains(string(output), testClusterName) {
		t.Logf("Deleting stale kind cluster %s from a previous run...", testClusterName)
		_ = exec.Command("kind", "delete", "cluster", "--name", testClusterName).Run()
	}

	t.Logf("Creating kind cluster %s...", testClusterName)
	cmd = exec.Command("kind", "create", "cluster", "--name", testClusterName)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to create kind cluster: %s", string(out))

	t.Log("Waiting for cluster to be ready...")
	for i := 0; i < 30; i++ {
		if exec.Command("kubectl", "cluster-info", "--context", kindCtx).Run() == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	require.NoError(t, exec.Command("kubectl", "cluster-info", "--context", kindCtx).Run(), "Cluster not ready after 60 seconds")
}

// teardownKindCluster deletes the kind cluster
func teardownKindCluster(t *testing.T) {
	t.Helper()
	t.Logf("Deleting kind cluster %s...", testClusterName)
	_ = exec.Command("kind", "delete", "cluster", "--name", testClusterName).Run()
}

// installMetalLB installs MetalLB for LoadBalancer support in Kind.
// This is test-only infrastructure — production clusters use real cloud LBs.
func installMetalLB(t *testing.T) {
	t.Helper()
	t.Log("Installing MetalLB for LoadBalancer support...")

	cmd := exec.Command("kubectl", "apply", "-f", "https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to install MetalLB: %s", string(out))

	t.Log("Waiting for MetalLB controller to be ready...")
	cmd = exec.Command("kubectl", "wait", "--namespace", "metallb-system",
		"--for=condition=available", "deployment/controller", "--timeout=120s")
	require.NoError(t, cmd.Run(), "MetalLB controller not ready")

	t.Log("Waiting for MetalLB speaker to be ready...")
	cmd = exec.Command("kubectl", "wait", "--namespace", "metallb-system",
		"--for=condition=ready", "pod", "-l", "component=speaker", "--timeout=120s")
	require.NoError(t, cmd.Run(), "MetalLB speaker not ready")

	// Discover the Docker network subnet for this Kind cluster so MetalLB
	// allocates IPs the host can actually reach.
	subnetOut, err := exec.Command("docker", "network", "inspect", "kind",
		"-f", `{{range .IPAM.Config}}{{.Subnet}} {{end}}`).Output()
	require.NoError(t, err, "Failed to inspect Kind Docker network")
	var subnet string
	for _, s := range strings.Fields(strings.TrimSpace(string(subnetOut))) {
		if strings.Contains(s, ".") {
			subnet = s
			break
		}
	}
	require.NotEmpty(t, subnet, "No IPv4 subnet found in Kind Docker network: %s", string(subnetOut))
	parts := strings.Split(subnet, ".")
	require.GreaterOrEqual(t, len(parts), 2, "Unexpected subnet format: %s", subnet)
	metallbRange := fmt.Sprintf("%s.%s.255.200-%s.%s.255.250", parts[0], parts[1], parts[0], parts[1])
	t.Logf("Kind network subnet: %s, MetalLB range: %s", subnet, metallbRange)

	ipPool := fmt.Sprintf(`apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-pool
  namespace: metallb-system
spec:
  addresses:
  - %s
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default
  namespace: metallb-system
spec:
  ipAddressPools:
  - default-pool
`, metallbRange)
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(ipPool)
	require.NoError(t, cmd.Run(), "Failed to configure MetalLB IP pool")
}

// dumpClusterDiagnostics logs pod status, events, and logs on failure.
func dumpClusterDiagnostics(t *testing.T) {
	t.Helper()
	if !t.Failed() {
		return
	}
	t.Log("=== CLUSTER DIAGNOSTICS (test failed) ===")

	if out, err := exec.Command("kubectl", "get", "pods", "-A", "-o", "wide").CombinedOutput(); err == nil {
		t.Logf("--- pods ---\n%s", string(out))
	}
	if out, err := exec.Command("kubectl", "get", "events", "--sort-by=.lastTimestamp", "-A").CombinedOutput(); err == nil {
		t.Logf("--- events (last 40) ---\n%s", lastNLines(string(out), 40))
	}
	if out, err := exec.Command("kubectl", "get", "gateway,httproute,grpcroute", "-o", "wide").CombinedOutput(); err == nil {
		t.Logf("--- gateway resources ---\n%s", string(out))
	}
	if out, err := exec.Command("kubectl", "get", "svc", "-A").CombinedOutput(); err == nil {
		t.Logf("--- services ---\n%s", string(out))
	}
	for _, ns := range []string{"default", "envoy-gateway-system"} {
		podOut, _ := exec.Command("kubectl", "get", "pods", "-n", ns,
			"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}").Output()
		for _, pod := range strings.Split(strings.TrimSpace(string(podOut)), "\n") {
			if pod == "" {
				continue
			}
			out, _ := exec.Command("kubectl", "logs", "-n", ns, pod, "--all-containers", "--tail=20").CombinedOutput()
			t.Logf("--- logs %s/%s ---\n%s", ns, pod, string(out))
		}
	}
	t.Log("=== END DIAGNOSTICS ===")
}

func lastNLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// getEnvoyIP polls for the Envoy proxy LoadBalancer IP.
func getEnvoyIP(t *testing.T) string {
	t.Helper()
	t.Log("Waiting for Envoy proxy external IP...")
	var envoyIP string
	for i := 0; i < 60; i++ {
		cmd := exec.Command("kubectl", "get", "svc", "-n", "envoy-gateway-system",
			"-l", "gateway.envoyproxy.io/owning-gateway-name=frkr-gateway",
			"-o", "jsonpath={.items[0].status.loadBalancer.ingress[0].ip}")
		output, err := cmd.Output()
		if err == nil {
			ip := strings.TrimSpace(string(output))
			if ip != "" && ip != "<pending>" {
				envoyIP = ip
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	require.NotEmpty(t, envoyIP, "Envoy proxy LoadBalancer IP not assigned")
	t.Logf("Envoy proxy IP: %s", envoyIP)
	return envoyIP
}

// verifyGatewayHealth checks if a gateway is healthy via HTTP.
func verifyGatewayHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	var lastErr string
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if err != nil {
			lastErr = fmt.Sprintf("error: %v", err)
		} else {
			lastErr = fmt.Sprintf("status: %d", resp.StatusCode)
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	require.Fail(t, "Gateway health check failed", "Could not reach %s within %v (last attempt: %s)", url, timeout, lastErr)
}

// verifyGRPCHealth checks the gRPC health service at the given address.
func verifyGRPCHealth(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := grpc.DialContext(ctx, addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			cancel()
			time.Sleep(2 * time.Second)
			continue
		}
		client := healthpb.NewHealthClient(conn)
		resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
		conn.Close()
		cancel()
		if err == nil && resp.Status == healthpb.HealthCheckResponse_SERVING {
			return
		}
		time.Sleep(2 * time.Second)
	}
	require.Fail(t, "gRPC health check failed", "Could not reach gRPC health at %s within %v", addr, timeout)
}

// ---------------------------------------------------------------------------
// newTestKubernetesManager builds a KubernetesManager configured for testing
// in a Kind cluster. The returned manager uses real production code paths.
// Accepts optional config overrides applied after defaults.
// ---------------------------------------------------------------------------

func newTestKubernetesManager(externalAccess string, overrides ...func(*FrkrupConfig)) *KubernetesManager {
	config := &FrkrupConfig{
		K8s:              true,
		Target:           "k8s",
		ExternalAccess:   externalAccess,
		ImageLoadCommand: "kind load docker-image --name " + testClusterName,
		SkipPortForward:  true,
	}
	for _, o := range overrides {
		o(config)
	}
	applyDefaults(config)
	return NewKubernetesManager(config)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestKubernetesExternalAccess_Ingress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	// Test-only infrastructure
	setupKindCluster(t)
	t.Cleanup(func() { teardownKindCluster(t) })
	t.Cleanup(func() { dumpClusterDiagnostics(t) })
	installMetalLB(t)

	// Production code paths via KubernetesManager
	km := newTestKubernetesManager("ingress")

	require.NoError(t, km.installGatewayAPICRDs())
	require.NoError(t, km.installEnvoyGateway())

	images, err := km.buildAndLoadImages()
	require.NoError(t, err)

	require.NoError(t, km.installHelmChart(images))
	require.NoError(t, km.waitForReadiness())

	// Assertions
	envoyIP := getEnvoyIP(t)
	verifyGatewayHealth(t, fmt.Sprintf("http://%s/health", envoyIP), 60*time.Second)
	verifyGRPCHealth(t, fmt.Sprintf("%s:80", envoyIP), 60*time.Second)

	t.Log("Ingress test passed!")
}

func TestKubernetesExternalAccess_ClusterIP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	// Test-only infrastructure
	setupKindCluster(t)
	t.Cleanup(func() { teardownKindCluster(t) })
	t.Cleanup(func() { dumpClusterDiagnostics(t) })

	// Production code paths via KubernetesManager
	km := newTestKubernetesManager("")

	require.NoError(t, km.installGatewayAPICRDs())

	images, err := km.buildAndLoadImages()
	require.NoError(t, err)

	require.NoError(t, km.installHelmChart(images))
	require.NoError(t, km.waitForReadiness())

	// Verify all services are ClusterIP
	t.Log("Testing ClusterIP configuration...")
	cmd := exec.Command("kubectl", "get", "svc", "-l", "app.kubernetes.io/name=frkr",
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.spec.type}{\"\\n\"}{end}")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to get services")

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "Expected at least 2 services")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			require.Equal(t, "ClusterIP", parts[1], "Service %s should be ClusterIP", parts[0])
		}
	}

	// Verify services are accessible via port-forward
	t.Log("Verifying internal access via port-forward...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ingestPF := exec.CommandContext(ctx, "kubectl", "port-forward", "svc/frkr-ingest-gateway", "18080:8080")
	require.NoError(t, ingestPF.Start())
	defer ingestPF.Process.Kill()
	time.Sleep(2 * time.Second)

	verifyGatewayHealth(t, "http://localhost:18080/health", 10*time.Second)

	t.Log("ClusterIP test passed!")
}

// TestKubernetesExternalAccess_IngressWithCertManager exercises the
// installCertManager() production code path and verifies that the Helm
// chart's ClusterIssuer and Certificate templates render and apply
// successfully. The ACME challenge won't complete in Kind (no real DNS),
// but we verify the full install pipeline and resource creation.
func TestKubernetesExternalAccess_IngressWithCertManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	require.NoError(t, exec.Command("kind", "version").Run(), "kind not installed")
	require.NoError(t, exec.Command("kubectl", "version", "--client").Run(), "kubectl not installed")
	require.NoError(t, exec.Command("helm", "version").Run(), "helm not installed")

	// Test-only infrastructure
	setupKindCluster(t)
	t.Cleanup(func() { teardownKindCluster(t) })
	t.Cleanup(func() { dumpClusterDiagnostics(t) })
	installMetalLB(t)

	// Production code paths via KubernetesManager — with cert-manager enabled
	km := newTestKubernetesManager("ingress", func(c *FrkrupConfig) {
		c.InstallCertManager = true
		c.CertManagerEmail = "test@example.com"
		c.IngressHost = "test.example.com"
		c.IngressTLSSecret = "frkr-tls"
	})

	require.NoError(t, km.installGatewayAPICRDs())
	require.NoError(t, km.installEnvoyGateway())
	require.NoError(t, km.installCertManager())

	images, err := km.buildAndLoadImages()
	require.NoError(t, err)

	require.NoError(t, km.installHelmChart(images))
	require.NoError(t, km.waitForReadiness())

	// Verify cert-manager CRDs exist
	t.Log("Verifying cert-manager CRDs...")
	for _, crd := range []string{
		"certificates.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
	} {
		require.NoError(t,
			exec.Command("kubectl", "get", "crd", crd).Run(),
			"cert-manager CRD %s not found", crd)
	}

	// Verify ClusterIssuer was created by the Helm chart
	t.Log("Verifying ClusterIssuer...")
	out, err := exec.Command("kubectl", "get", "clusterissuer", "letsencrypt-prod",
		"-o", "jsonpath={.metadata.name}").Output()
	require.NoError(t, err, "ClusterIssuer not found")
	require.Equal(t, "letsencrypt-prod", strings.TrimSpace(string(out)))

	// Verify Certificate was created by the Helm chart
	t.Log("Verifying Certificate...")
	out, err = exec.Command("kubectl", "get", "certificate", "frkr-tls-cert",
		"-o", "jsonpath={.spec.dnsNames[0]}").Output()
	require.NoError(t, err, "Certificate not found")
	require.Equal(t, "test.example.com", strings.TrimSpace(string(out)))

	// Gateway routing is already tested in TestKubernetesExternalAccess_Ingress.
	// This test's purpose is verifying the cert-manager install pipeline.
	// (Health checks would need Host header matching since IngressHost is set.)

	t.Log("Ingress with cert-manager test passed!")
}
