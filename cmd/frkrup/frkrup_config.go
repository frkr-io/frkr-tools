package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MockOIDCIssuerURL is the internal K8s URL for the mock OIDC provider
const MockOIDCIssuerURL = "http://frkr-mock-oidc.default.svc.cluster.local:8080/default"

// FrkrupConfig holds the configuration for frkrup setup
type FrkrupConfig struct {
	// Deployment mode
	Target           string `yaml:"target"` // "local" or "k8s"
	K8s              bool   `yaml:"k8s"`    // Deprecated: use target="k8s"
	K8sClusterName   string `yaml:"k8s_cluster_name"`
	SkipPortForward  bool   `yaml:"skip_port_forward"`
	ExternalAccess       string `yaml:"external_access"` // "none" or "ingress"
	IngressHost          string `yaml:"ingress_host"`
	IngestIngressHost    string `yaml:"ingest_ingress_host"`
	StreamingIngressHost string `yaml:"streaming_ingress_host"`
	IngressTLSSecret     string `yaml:"ingress_tls_secret"`
	PortForwardAddress string `yaml:"port_forward_address"`
	ImageRegistry      string `yaml:"image_registry"`
	ImageLoadCommand   string `yaml:"image_load_command"` // Shell command to load a local image into the cluster (e.g. "kind load docker-image --name my-cluster")
	Rebuild            bool   `yaml:"-"`                  // Flag to force build/push (CLI only)

	// Vendor binding
	Provider string `yaml:"provider"`

	// TLS/CertManager configuration
	InstallCertManager bool   `yaml:"install_cert_manager"`
	CertManagerEmail   string `yaml:"cert_manager_email"`
	CertIssuerName     string `yaml:"cert_issuer_name"`
	CertIssuerServer   string `yaml:"cert_issuer_server"`
	IngressClassName   string `yaml:"ingress_class_name"`

	// Database configuration
	DBHost     string `yaml:"db_host"`
	DBPort     string `yaml:"db_port"`
	DBUser     string `yaml:"db_user"`
	DBPassword string `yaml:"db_password"`
	DBName     string `yaml:"db_name"`

	// Broker configuration
	BrokerHost     string `yaml:"broker_host"`
	BrokerPort     string `yaml:"broker_port"`
	BrokerUser     string `yaml:"broker_user"`
	BrokerPassword string `yaml:"broker_password"`

	// Gateway configuration
	IngestPort    int    `yaml:"ingest_port"`
	StreamingPort int    `yaml:"streaming_port"`
	IngestHost    string `yaml:"ingest_host"`
	StreamingHost string `yaml:"streaming_host"`

	// Test configuration
	TestOIDC bool `yaml:"test_oidc"`
	
	// Real OIDC Configuration
	OidcIssuer       string `yaml:"oidc_issuer"`
	OidcClientId     string `yaml:"oidc_client_id"`
	OidcClientSecret string `yaml:"oidc_client_secret"`

	// Paths
	MigrationsPath string `yaml:"migrations_path"`

	// Runtime state (not from YAML)
	StartedDocker bool      `yaml:"-"`
	IngestCmd     *exec.Cmd `yaml:"-"`
	StreamingCmd  *exec.Cmd `yaml:"-"`
}

// loadConfigFromFile loads configuration from a YAML file
func loadConfigFromFile(path string) (*FrkrupConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &FrkrupConfig{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults for unset values
	applyDefaults(config)

	// Validate required fields
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

// validateConfig validates that required configuration is present
func validateConfig(config *FrkrupConfig) error {
	if !config.K8s {
		// Local mode requires explicit host configuration
		if config.DBHost == "" {
			return fmt.Errorf("db_host is required in config file")
		}
		if config.BrokerHost == "" {
			return fmt.Errorf("broker_host is required in config file")
		}
	}

	// Ingress host mutual exclusion:
	// - ingress_host (shared/path routing) and per-service hosts (subdomain routing) cannot be mixed
	// - per-service hosts must be specified as a pair
	hasSharedHost := config.IngressHost != ""
	hasIngestHost := config.IngestIngressHost != ""
	hasStreamingHost := config.StreamingIngressHost != ""

	if hasSharedHost && (hasIngestHost || hasStreamingHost) {
		return fmt.Errorf("ingress_host and per-service hosts (ingest_ingress_host, streaming_ingress_host) are mutually exclusive")
	}
	if hasIngestHost != hasStreamingHost {
		return fmt.Errorf("both ingest_ingress_host and streaming_ingress_host must be specified together")
	}

	return nil
}

// applyDefaults sets default values for unset config fields
func applyDefaults(config *FrkrupConfig) {
	// 0. Normalize Target
	if config.Target == "" {
		if config.K8s {
			config.Target = "k8s"
		} else {
			config.Target = "local"
		}
	}
	// Sync K8s bool for internal logic
	config.K8s = (config.Target == "k8s")

	// 1. Smart Defaults for Remote Clusters
	// If an Image Registry is configured, we assume this is a remote deployment (AKS/EKS/etc)
	// and therefore we should NOT try to port-forward by default unless explicitly asked.
	isRemote := config.ImageRegistry != ""
	if isRemote && !config.SkipPortForward {
		// User didn't explicit set skip_port_forward, so we default it to TRUE for remote
		// (We need a way to differentiate "User set false" vs "Default false", but bool defaults to false.
		// For now, if registry is set, we assume you want to skip PF. If you really want PF on remote, you can't easily express that
		// without a tristate or explicit flag. But remote PF is rare/dangerous anyway.)
		config.SkipPortForward = true
		fmt.Println("remote deployment detected (registry set) -> disabling port forwarding")
	}

	// Note: DBHost and BrokerHost are REQUIRED - no defaults
	// They must be explicitly set in config file or via prompts
	if config.DBHost == "" && config.K8s {
		config.DBHost = "frkr-db"
	}
	if config.BrokerHost == "" && config.K8s {
		config.BrokerHost = "frkr-redpanda"
	}

	if config.DBPort == "" {
		config.DBPort = "5432"
	}
	if config.DBUser == "" {
		config.DBUser = "root"
	}
	if config.DBName == "" {
		config.DBName = "frkr"
	}
	if config.BrokerPort == "" {
		config.BrokerPort = "19092"
	}
	if config.IngestPort == 0 {
		config.IngestPort = 8082
	}
	if config.StreamingPort == 0 {
		config.StreamingPort = 8081
	}

	if config.PortForwardAddress == "" {
		config.PortForwardAddress = "localhost"
	}

	if config.CertIssuerName == "" {
		config.CertIssuerName = "letsencrypt-prod"
	}
	if config.CertIssuerServer == "" {
		config.CertIssuerServer = "https://acme-v02.api.letsencrypt.org/directory"
	}

	if config.MigrationsPath == "" {
		// Use the robust path finder that uses Go modules
		path, err := findMigrationsPath()
		fmt.Printf("[DEBUG] findMigrationsPath returned: %q (err=%v)\n", path, err)
		if err == nil {
			config.MigrationsPath = path
		} else {
			// Fallback: try local candidates
			candidates := []string{
				"frkr-common/migrations",
				"../frkr-common/migrations",
			}
			for _, candidate := range candidates {
				absPath, err := filepath.Abs(candidate)
				fmt.Printf("[DEBUG] Checking candidate: %s (abs: %s, err: %v)\n", candidate, absPath, err)
				if err == nil {
					if _, err := os.Stat(absPath); err == nil {
						fmt.Printf("[DEBUG] Found valid candidate: %s\n", absPath)
						config.MigrationsPath = absPath
						break
					}
				}
			}
		}
	}
}

// BuildDBURL constructs a PostgreSQL connection URL from the config
func (c *FrkrupConfig) BuildDBURL() string {
	if c.DBUser != "" {
		if c.DBPassword != "" {
			return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
		}
		return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
			c.DBUser, c.DBHost, c.DBPort, c.DBName)
	}
	return fmt.Sprintf("postgres://%s:%s/%s?sslmode=disable",
		c.DBHost, c.DBPort, c.DBName)
}

// BuildBrokerURL constructs a broker connection URL from the config
func (c *FrkrupConfig) BuildBrokerURL() string {
	return fmt.Sprintf("%s:%s", c.BrokerHost, c.BrokerPort)
}

// ingressScheme returns "https" if TLS is configured, "http" otherwise.
func (c *FrkrupConfig) ingressScheme() string {
	if c.IngressTLSSecret != "" {
		return "https"
	}
	return "http"
}

// effectiveIngestHost returns the hostname for the ingest gateway through the ingress.
// Prefers the per-service host (subdomain routing) over the shared host (path routing).
func (c *FrkrupConfig) effectiveIngestHost() string {
	if c.IngestIngressHost != "" {
		return c.IngestIngressHost
	}
	return c.IngressHost
}

// effectiveStreamingHost returns the hostname for the streaming gateway through the ingress.
// Prefers the per-service host (subdomain routing) over the shared host (path routing).
func (c *FrkrupConfig) effectiveStreamingHost() string {
	if c.StreamingIngressHost != "" {
		return c.StreamingIngressHost
	}
	return c.IngressHost
}

// BuildIngestGatewayURL constructs the URL for ingest gateway health checks.
// Deployment scenarios:
// - Local: http://localhost:<IngestPort>/health
// - K8s port-forward: http://localhost:<IngestPort>/health
// - K8s Ingress: http(s)://<hostname>/health (the /health HTTPRoute goes to ingest gateway)
func (c *FrkrupConfig) BuildIngestGatewayURL() string {
	// For Ingress mode, use the effective hostname with /health path.
	// The Envoy Gateway has an explicit /health HTTPRoute pointing to the ingest gateway.
	if c.K8s && c.ExternalAccess == "ingress" && c.effectiveIngestHost() != "" {
		return fmt.Sprintf("%s://%s/health", c.ingressScheme(), c.effectiveIngestHost())
	}

	host := c.IngestHost
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d/health", host, c.IngestPort)
}

// BuildStreamingGatewayURL constructs the URL for streaming gateway health checks.
// NOTE: The streaming gateway is gRPC-based. Its HTTP health endpoint (/health) is on
// port+1000 (a sidecar for local dev metrics). In K8s, health is checked by native gRPC
// readiness probes on the pod.
//
// For Ingress mode, we return the ingest gateway's /health endpoint as a system-level
// health proxy. The streaming gateway only serves gRPC, so an HTTP health check against
// it would fail. The K8s gRPC readiness probes are the authoritative health signal for
// the streaming gateway.
func (c *FrkrupConfig) BuildStreamingGatewayURL() string {
	// For Ingress mode, always use the ingest host's /health endpoint.
	// The streaming gateway doesn't serve HTTP, so we check the ingest gateway
	// as a proxy for "the ingress infrastructure is working."
	if c.K8s && c.ExternalAccess == "ingress" {
		host := c.effectiveIngestHost()
		if host != "" {
			return fmt.Sprintf("%s://%s/health", c.ingressScheme(), host)
		}
	}

	host := c.StreamingHost
	if host == "" {
		host = "localhost"
	}

	// Local mode: streaming gateway serves HTTP health on gRPC port + 1000
	port := c.StreamingPort
	if !c.K8s {
		port = c.StreamingPort + 1000
	}
	return fmt.Sprintf("http://%s:%d/health", host, port)
}
