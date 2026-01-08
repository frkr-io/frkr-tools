package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FrkrupFrkrupConfig holds the configuration for frkrup setup
type FrkrupFrkrupConfig struct {
	// Deployment mode
	K8s              bool   `yaml:"k8s"`
	K8sClusterName   string `yaml:"k8s_cluster_name"`
	SkipPortForward  bool   `yaml:"skip_port_forward"`
	ExternalAccess   string `yaml:"external_access"` // "none", "loadbalancer", "ingress"
	IngressHost      string `yaml:"ingress_host"`
	IngressTLSSecret string `yaml:"ingress_tls_secret"`

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

	// Stream configuration
	StreamName   string `yaml:"stream_name"`
	CreateStream bool   `yaml:"create_stream"`

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

// validateFrkrupConfig validates that required configuration is present
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
	return nil
}

// applyDefaults sets default values for unset config fields
func applyDefaults(config *FrkrupConfig) {
	// Note: DBHost and BrokerHost are REQUIRED - no defaults
	// They must be explicitly set in config file or via prompts
	if config.DBPort == "" {
		config.DBPort = "26257"
	}
	if config.DBUser == "" {
		config.DBUser = "root"
	}
	if config.DBName == "" {
		config.DBName = "frkrdb"
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
	if config.StreamName == "" {
		config.StreamName = "my-api"
	}
	if config.MigrationsPath == "" {
		// Use the robust path finder that uses Go modules
		if path, err := findMigrationsPath(); err == nil {
			config.MigrationsPath = path
		} else {
			// Fallback: try local candidates (for development when go.mod isn't set up yet)
			candidates := []string{
				"frkr-common/migrations",
				"../frkr-common/migrations",
				"frkr-infra-helm/migrations",
			}
			for _, candidate := range candidates {
				if absPath, err := filepath.Abs(candidate); err == nil {
					if _, err := os.Stat(absPath); err == nil {
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

// BuildIngestGatewayURL constructs the URL for ingest gateway health checks
// The host is set by the orchestrator based on deployment scenario:
// - Local: localhost
// - K8s port-forward: localhost  
// - K8s LoadBalancer: external IP (set when IP is assigned)
// - K8s Ingress: ingress hostname with path prefix
func (c *FrkrupConfig) BuildIngestGatewayURL() string {
	host := c.IngestHost
	if host == "" {
		host = "localhost"
	}
	
	// For Ingress, use hostname with path prefix
	if c.K8s && c.ExternalAccess == "ingress" && c.IngressHost != "" {
		return fmt.Sprintf("http://%s/ingest/health", c.IngressHost)
	}
	
	// Standard health endpoint
	port := c.IngestPort
	if c.K8s && c.ExternalAccess == "loadbalancer" {
		port = 8080 // K8s service port
	}
	return fmt.Sprintf("http://%s:%d/health", host, port)
}

// BuildStreamingGatewayURL constructs the URL for streaming gateway health checks
func (c *FrkrupConfig) BuildStreamingGatewayURL() string {
	host := c.StreamingHost
	if host == "" {
		host = "localhost"
	}
	
	// For Ingress, use hostname with path prefix
	if c.K8s && c.ExternalAccess == "ingress" && c.IngressHost != "" {
		return fmt.Sprintf("http://%s/streaming/health", c.IngressHost)
	}
	
	// Standard health endpoint
	port := c.StreamingPort
	if c.K8s && c.ExternalAccess == "loadbalancer" {
		port = 8081 // K8s service port
	}
	return fmt.Sprintf("http://%s:%d/health", host, port)
}
