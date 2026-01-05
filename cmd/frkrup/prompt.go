package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// promptConfig prompts the user for configuration values
func promptConfig() (*Config, error) {
	config := &Config{
		IngestPort:    8082,
		StreamingPort: 8081,
		StreamName:    "my-api",
		CreateStream:  true,
	}

	scanner := bufio.NewScanner(os.Stdin)

	// K8s?
	fmt.Print("Deploy to Kubernetes? (yes/no) [no]: ")
	scanner.Scan()
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	config.K8s = answer == "yes" || answer == "y"

	if config.K8s {
		// Automatically determine migrations path using Go module resolution
		migrationsPath, err := findMigrationsPath()
		if err != nil {
			return nil, fmt.Errorf("failed to find migrations: %w", err)
		}
		config.MigrationsPath = migrationsPath
		return config, nil
	}

	// Database configuration
	fmt.Print("Database host [localhost]: ")
	scanner.Scan()
	config.DBHost = strings.TrimSpace(scanner.Text())
	if config.DBHost == "" {
		config.DBHost = "localhost"
	}

	fmt.Print("Database port [26257]: ")
	scanner.Scan()
	config.DBPort = strings.TrimSpace(scanner.Text())
	if config.DBPort == "" {
		config.DBPort = "26257"
	}

	fmt.Print("Database user (optional) [root]: ")
	scanner.Scan()
	config.DBUser = strings.TrimSpace(scanner.Text())
	if config.DBUser == "" {
		config.DBUser = "root"
	}

	fmt.Print("Database password (optional): ")
	scanner.Scan()
	config.DBPassword = strings.TrimSpace(scanner.Text())

	// Database name is hard-coded to "frkrdb" to match gateways and other components
	config.DBName = "frkrdb"

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

	// Gateway ports
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

	// Stream name
	fmt.Printf("Stream name [%s]: ", config.StreamName)
	scanner.Scan()
	streamName := strings.TrimSpace(scanner.Text())
	if streamName != "" {
		config.StreamName = streamName
	}

	// Automatically determine migrations path using Go module resolution
	migrationsPath, err := findMigrationsPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find migrations: %w", err)
	}
	config.MigrationsPath = migrationsPath

	return config, nil
}
