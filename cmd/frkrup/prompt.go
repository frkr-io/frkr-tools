package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// promptFrkrupConfig prompts the user for configuration values
func promptConfig() (*FrkrupConfig, error) {
	config := &FrkrupConfig{
		IngestPort:    8082,
		StreamingPort: 8081,
	}

	scanner := bufio.NewScanner(os.Stdin)

	// Auto-detect if kubectl is available and connected to a cluster
	isK8sAvailable := isKubernetesAvailable()
	
	// K8s?
	if isK8sAvailable {
		fmt.Print("Deploy to Kubernetes? (yes/no) [yes]: ")
	} else {
		fmt.Print("Deploy to Kubernetes? (yes/no) [no]: ")
	}
	scanner.Scan()
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer == "" {
		// Default based on availability
		config.K8s = isK8sAvailable
	} else {
		config.K8s = answer == "yes" || answer == "y"
	}

	if config.K8s {
		// Auto-detect if we're in a kind cluster
		isKindCluster := isKindCluster()
		
		// Auto-detect port forwarding need (default to yes for kind, no for managed)
		if isKindCluster {
			fmt.Print("Use port forwarding for local access? (yes/no) [yes]: ")
		} else {
			fmt.Print("Use port forwarding for local access? (yes/no) [no]: ")
		}
		scanner.Scan()
		answer = strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "" {
			config.SkipPortForward = !isKindCluster // Default: yes for kind, no for managed
		} else {
			config.SkipPortForward = answer == "no" || answer == "n"
		}

		// Ask about external access configuration only if not using port forwarding
		if config.SkipPortForward {
			fmt.Println("\n📡 External Access Configuration:")
			fmt.Println("   How should gateways be exposed externally?")
			fmt.Println("   1. LoadBalancer (cloud providers - EKS, GKE, AKS)")
			fmt.Println("   2. Ingress (requires Ingress controller)")
			fmt.Println("   3. None (ClusterIP only, internal access)")
			fmt.Print("   Choose option (1/2/3) [1]: ")
			scanner.Scan()
			choice := strings.TrimSpace(scanner.Text())
			if choice == "" {
				choice = "1"
			}

			switch choice {
			case "1":
				config.ExternalAccess = "loadbalancer"
			case "2":
				config.ExternalAccess = "ingress"
				fmt.Print("   Ingress hostname (e.g., frkr.example.com): ")
				scanner.Scan()
				config.IngressHost = strings.TrimSpace(scanner.Text())
				if config.IngressHost == "" {
					return nil, fmt.Errorf("ingress hostname is required")
				}
				fmt.Print("   TLS Secret Name (optional, press Enter to skip): ")
				scanner.Scan()
				config.IngressTLSSecret = strings.TrimSpace(scanner.Text())
			case "3":
				config.ExternalAccess = "none"
			default:
				return nil, fmt.Errorf("invalid choice: %s", choice)
			}
		} else {
			config.ExternalAccess = "none" // Port forwarding, no external access needed
		}

		// Automatically determine migrations path using Go module resolution
		migrationsPath, err := findMigrationsPath()
		if err != nil {
			return nil, fmt.Errorf("failed to find migrations: %w", err)
		}
		config.MigrationsPath = migrationsPath

		// Set default hosts/ports for K8s (they will be port-forwarded)
		config.DBHost = "localhost"
		config.DBPort = "5432"
		config.DBName = "frkr"
		config.BrokerHost = "localhost"

		fmt.Println("\n🔑 Database Credentials (will be set in Kubernetes Secrets):")
		fmt.Print("   Database User [root]: ")
		scanner.Scan()
		config.DBUser = strings.TrimSpace(scanner.Text())
		if config.DBUser == "" {
			config.DBUser = "root"
		}
		
		// Password with verification
		config.DBPassword, err = promptPasswordWithConfirmation("   Database Password [Required]: ")
		if err != nil {
			return nil, err
		}

		return config, nil
	}

	// Auto-detect if services are running using actual service checkers
	// This is more reliable than port checking - we verify services actually work
	dbURL := "postgres://root:password@localhost:5432/frkr?sslmode=disable"
	brokerURL := "localhost:19092"
	
	dbChecker := NewDatabaseChecker()
	brokerChecker := NewBrokerChecker()
	
	// Quick check if services are ready (with short timeout to avoid blocking)
	dbReady := dbChecker.Check(dbURL) == nil
	brokerReady := brokerChecker.Check(brokerURL) == nil
	
	if dbReady && brokerReady {
		fmt.Println("✅ Detected running services on default ports")
		fmt.Println("   Database: localhost:5432")
		fmt.Println("   Broker: localhost:19092")
		fmt.Print("\nUse detected services? (yes/no) [yes]: ")
		scanner.Scan()
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "" || answer == "yes" || answer == "y" {
			// Use defaults
			config.DBHost = "localhost"
			config.DBPort = "5432"
			config.DBUser = "root"
			config.DBPassword = "password"
			config.DBName = "frkr"
			config.BrokerHost = "localhost"
			config.BrokerPort = "19092"
			config.BrokerUser = ""
			config.BrokerPassword = ""
		} else {
			// Prompt for custom values
			config = promptCustomInfrastructure(config, scanner)
		}
	} else {
		// Services not detected, use defaults but allow customization
		fmt.Println("⚠️  Services not detected on default ports")
		fmt.Print("Use default configuration? (yes/no) [yes]: ")
		scanner.Scan()
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "" || answer == "yes" || answer == "y" {
			// Use defaults
			config.DBHost = "localhost"
			config.DBPort = "5432"
			config.DBUser = "root"
			config.DBPassword = "password"
			config.DBName = "frkr"
			config.BrokerHost = "localhost"
			config.BrokerPort = "19092"
			config.BrokerUser = ""
			config.BrokerPassword = ""
		} else {
			// Prompt for custom values
			config = promptCustomInfrastructure(config, scanner)
		}
	}

	// Gateway ports (only prompt if non-default needed)
	fmt.Printf("Ingest gateway port [%d]: ", config.IngestPort)
	scanner.Scan()
	portStr := strings.TrimSpace(scanner.Text())
	if portStr != "" {
		fmt.Sscanf(portStr, "%d", &config.IngestPort)
	}

	fmt.Printf("Streaming gateway port [%d]: ", config.StreamingPort)
	scanner.Scan()
	portStr = strings.TrimSpace(scanner.Text())
	if portStr != "" {
		fmt.Sscanf(portStr, "%d", &config.StreamingPort)
	}

	// Test OIDC Configuration
	fmt.Print("Enable Test OIDC Provider? (yes/no) [no]: ")
	scanner.Scan()
	answer = strings.ToLower(strings.TrimSpace(scanner.Text()))
	config.TestOIDC = answer == "yes" || answer == "y"

	// Automatically determine migrations path using Go module resolution
	migrationsPath, err := findMigrationsPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find migrations: %w", err)
	}
	config.MigrationsPath = migrationsPath

	return config, nil
}

// promptCustomInfrastructure prompts for custom infrastructure configuration
func promptCustomInfrastructure(config *FrkrupConfig, scanner *bufio.Scanner) *FrkrupConfig {
	fmt.Println("\n📋 Custom Infrastructure Configuration:")
	
	// Database configuration
	fmt.Print("Database host [localhost]: ")
	scanner.Scan()
	config.DBHost = strings.TrimSpace(scanner.Text())
	if config.DBHost == "" {
		config.DBHost = "localhost"
	}

	fmt.Print("Database port [5432]: ")
	scanner.Scan()
	config.DBPort = strings.TrimSpace(scanner.Text())
	if config.DBPort == "" {
		config.DBPort = "5432"
	}

	fmt.Print("Database user (optional) [root]: ")
	scanner.Scan()
	config.DBUser = strings.TrimSpace(scanner.Text())
	if config.DBUser == "" {
		config.DBUser = "root"
	}

	// Database password
	if pass := promptPassword("Database password (optional) [password]: "); pass != "" {
		config.DBPassword = pass
	} else {
		config.DBPassword = "password"
	}

	// Database name is hard-coded to "frkr" to match gateways and other components
	config.DBName = "frkr"

	// Broker configuration
	fmt.Print("Broker host [localhost]: ")
	scanner.Scan()
	config.BrokerHost = strings.TrimSpace(scanner.Text())
	if config.BrokerHost == "" {
		config.BrokerHost = "localhost"
	}

	fmt.Print("Broker port [19092]: ")
	scanner.Scan()
	config.BrokerPort = strings.TrimSpace(scanner.Text())
	if config.BrokerPort == "" {
		config.BrokerPort = "19092"
	}

	fmt.Print("Broker user (optional): ")
	scanner.Scan()
	config.BrokerUser = strings.TrimSpace(scanner.Text())

	fmt.Print("Broker password (optional): ")
	scanner.Scan()
	config.BrokerPassword = strings.TrimSpace(scanner.Text())
	
	return config
}
// generateDefaultConfig creates a configuration using robust defaults without prompting
func generateDefaultConfig() (*FrkrupConfig, error) {
	config := &FrkrupConfig{
		IngestPort:    8082,
		StreamingPort: 8081,
	}

	// Auto-detect K8s
	if isKubernetesAvailable() {
		config.K8s = true
		
		// Auto-detect Kind
		isKind := isKindCluster()
		
		// Default port forwarding logic
		if isKind {
			config.SkipPortForward = false // Use port forwarding for Kind
		} else {
			config.SkipPortForward = true  // No port forwarding for others
		}

		if config.SkipPortForward {
			// specific defaults for non-port-forward
			config.ExternalAccess = "loadbalancer" // Default choice
		} else {
			config.ExternalAccess = "none"
		}

		// K8s defaults
		config.DBHost = "localhost"
		config.BrokerHost = "localhost"

	} else {
		// Local mode defaults
		config.K8s = false
		
		// Default services
		config.DBHost = "localhost"
		config.DBPort = "5432"
		config.DBUser = "root"
		config.DBPassword = "password"
		config.DBName = "frkr"
		config.BrokerHost = "localhost"
		config.BrokerPort = "19092"
		config.BrokerUser = ""
		config.BrokerPassword = ""
	}

	// Find migrations
	migrationsPath, err := findMigrationsPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find migrations: %w", err)
	}
	config.MigrationsPath = migrationsPath

	return config, nil
}

// promptPassword prompts for a password with masking
func promptPassword(label string) string {
	fmt.Print(label)
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return ""
	}
	fmt.Println("") // Newline after input
	return strings.TrimSpace(string(bytePassword))
}

// promptPasswordWithConfirmation prompts for a password with confirmation and retries
func promptPasswordWithConfirmation(label string) (string, error) {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// First input
		pass := promptPassword(label)
		if pass == "" {
			fmt.Println("Error: Password cannot be empty.")
			continue
		}

		// Confirmation
		confirm := promptPassword(strings.TrimSuffix(label, ": ") + " (Confirm): ")
		
		if pass == confirm {
			return pass, nil
		}
		
		fmt.Println("❌ Passwords do not match. Please try again.")
	}
	return "", fmt.Errorf("failed to verify password after %d attempts", maxRetries)
}
