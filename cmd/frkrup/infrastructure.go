package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"
)

// InfrastructureManager handles infrastructure setup and verification
type InfrastructureManager struct {
	config *Config
}

// NewInfrastructureManager creates a new InfrastructureManager
func NewInfrastructureManager(config *Config) *InfrastructureManager {
	return &InfrastructureManager{config: config}
}

// EnsureRunning checks if database and broker are running,
// and optionally starts Docker Compose if they're not available.
func (im *InfrastructureManager) EnsureRunning() error {
	// Quick check if services are already running
	dbRunning := isPortOpen(im.config.DBHost, im.config.DBPort)
	brokerRunning := isPortOpen(im.config.BrokerHost, im.config.BrokerPort)

	if dbRunning && brokerRunning {
		fmt.Println("✅ Infrastructure services are already running")
		return nil
	}

	// Services aren't running - check if we can start Docker Compose
	dockerPath, err := findInfraRepoPath("docker")
	if err != nil {
		fmt.Println("\n⚠️  Infrastructure services are not running.")
		fmt.Println("   Please start them manually or ensure they're accessible.")
		fmt.Printf("   Expected: Database at %s:%s, Broker at %s:%s\n",
			im.config.DBHost, im.config.DBPort, im.config.BrokerHost, im.config.BrokerPort)
		return fmt.Errorf("infrastructure not available and frkr-infra-docker not found")
	}

	// Check if docker-compose.yaml exists
	composeFile := filepath.Join(dockerPath, "docker-compose.yaml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		fmt.Println("\n⚠️  Infrastructure services are not running.")
		fmt.Println("   Please start them manually.")
		return fmt.Errorf("docker-compose.yaml not found in frkr-infra-docker")
	}

	// Offer to start Docker Compose
	fmt.Println("\n⚠️  Infrastructure services are not running.")
	fmt.Println("   frkr-infra-docker found. Would you like to start Docker Compose?")
	fmt.Print("   Start Docker Compose? (yes/no) [yes]: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer != "" && answer != "yes" && answer != "y" {
		return fmt.Errorf("infrastructure services are required")
	}

	// Start Docker Compose
	fmt.Println("\n🐳 Starting Docker Compose...")
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = dockerPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start Docker Compose: %w", err)
	}

	im.config.StartedDocker = true

	// Wait for services to be ready
	fmt.Println("\n⏳ Waiting for services to be ready...")

	// Build URLs for checking
	dbURL := im.config.BuildDBURL()
	brokerURL := im.config.BuildBrokerURL()

	maxWait := 90 // seconds
	for i := 0; i < maxWait; i++ {
		// Check if ports are open first (quick check)
		if !isPortOpen(im.config.DBHost, im.config.DBPort) || !isPortOpen(im.config.BrokerHost, im.config.BrokerPort) {
			time.Sleep(1 * time.Second)
			if (i+1)%10 == 0 {
				fmt.Printf("   Waiting for ports... (%d/%d seconds)\n", i+1, maxWait)
			}
			continue
		}

		// Ports are open, now verify services are actually ready
		dbChecker := NewDatabaseChecker()
		brokerChecker := NewBrokerChecker()
		dbReady := dbChecker.Check(dbURL) == nil
		brokerReady := brokerChecker.Check(brokerURL) == nil

		if dbReady && brokerReady {
			fmt.Println("✅ Services are ready")
			return nil
		}

		time.Sleep(2 * time.Second)
		if (i+1)%10 == 0 {
			status := []string{}
			if !dbReady {
				status = append(status, "database")
			}
			if !brokerReady {
				status = append(status, "broker")
			}
			fmt.Printf("   Waiting for %s... (%d/%d seconds)\n", strings.Join(status, " and "), i+1, maxWait)
		}
	}

	// Services didn't become ready - cleanup Docker before returning error
	cleanupDocker(im.config)
	return fmt.Errorf("services did not become ready within %d seconds", maxWait)
}

// isPortOpen checks if a TCP port is open and accepting connections
func isPortOpen(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// DatabaseChecker handles database connection verification
type DatabaseChecker struct{}

// NewDatabaseChecker creates a new DatabaseChecker
func NewDatabaseChecker() *DatabaseChecker {
	return &DatabaseChecker{}
}

// Check verifies that the database is accessible and creates it if needed
func (dc *DatabaseChecker) Check(dbURL string) error {
	fmt.Printf("🔍 [DEBUG] DatabaseChecker.Check called with dbURL: %s\n", maskPassword(dbURL))

	// First, try to connect to the target database
	fmt.Printf("🔍 [DEBUG] Attempting to connect to target database...\n")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("❌ [DEBUG] sql.Open failed: %v\n", err)
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Printf("🔍 [DEBUG] Pinging target database...\n")
	if err := db.PingContext(ctx); err != nil {
		fmt.Printf("⚠️  [DEBUG] Ping failed: %v\n", err)
		// If connection fails, the database might not exist yet
		// Try connecting to defaultdb to create the target database
		if strings.Contains(dbURL, "/frkrdb") {
			fmt.Printf("🔍 [DEBUG] Database doesn't exist, attempting to create it...\n")
			// Connect without specifying a database (connects to defaultdb)
			// Remove the database name from the URL
			baseURL := strings.Split(dbURL, "/")
			if len(baseURL) >= 3 {
				// Reconstruct URL without database name: postgres://user@host:port
				defaultURL := strings.Join(baseURL[:len(baseURL)-1], "/")
				// Remove query string if present
				if idx := strings.Index(defaultURL, "?"); idx != -1 {
					defaultURL = defaultURL[:idx]
				}
				fmt.Printf("🔍 [DEBUG] Connecting without database (will use defaultdb): %s\n", maskPassword(defaultURL))
				defaultDB, err := sql.Open("postgres", defaultURL)
				if err != nil {
					fmt.Printf("❌ [DEBUG] Failed to connect: %v\n", err)
					return fmt.Errorf("failed to connect to CockroachDB: %w", err)
				}
				defer defaultDB.Close()

				// Check if connection is ready
				fmt.Printf("🔍 [DEBUG] Pinging CockroachDB...\n")
				if err := defaultDB.PingContext(ctx); err != nil {
					fmt.Printf("❌ [DEBUG] CockroachDB not ready: %v\n", err)
					return fmt.Errorf("cockroachdb not ready yet: %w", err)
				}
				fmt.Printf("🔍 [DEBUG] CockroachDB is ready\n")

				// Create the frkrdb database (CockroachDB doesn't support IF NOT EXISTS)
				fmt.Printf("🔍 [DEBUG] Creating frkrdb database...\n")
				_, err = defaultDB.ExecContext(ctx, "CREATE DATABASE frkrdb")
				if err != nil {
					// If database already exists, that's fine - continue
					if strings.Contains(err.Error(), "already exists") ||
						strings.Contains(err.Error(), "duplicate") ||
						strings.Contains(err.Error(), "database \"frkrdb\" already exists") {
						fmt.Printf("🔍 [DEBUG] Database already exists, continuing...\n")
						// Database exists, that's fine
					} else {
						fmt.Printf("❌ [DEBUG] Failed to create database: %v\n", err)
						return fmt.Errorf("failed to create database: %w", err)
					}
				} else {
					fmt.Printf("🔍 [DEBUG] Database created successfully\n")
				}

				// Wait longer for database to be fully ready (CockroachDB needs time to initialize)
				fmt.Printf("🔍 [DEBUG] Waiting for database to be ready...\n")
				time.Sleep(2 * time.Second)

				// Now try connecting to frkrdb again with retries
				maxRetries := 10
				fmt.Printf("🔍 [DEBUG] Attempting to connect to frkrdb with %d retries...\n", maxRetries)
				for i := 0; i < maxRetries; i++ {
					fmt.Printf("🔍 [DEBUG] Retry %d/%d: Pinging frkrdb...\n", i+1, maxRetries)
					if err := db.PingContext(ctx); err == nil {
						fmt.Printf("🔍 [DEBUG] Successfully connected to frkrdb\n")
						// Connection successful - ensure public schema exists
						// CockroachDB should create it automatically, but let's be explicit
						fmt.Printf("🔍 [DEBUG] Ensuring public schema exists...\n")
						_, schemaErr := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS public")
						if schemaErr != nil {
							fmt.Printf("⚠️  [DEBUG] Could not ensure public schema: %v (may be okay)\n", schemaErr)
							// Schema creation failed, but this might be okay if it already exists
							// Continue anyway as the schema should exist
						} else {
							fmt.Printf("🔍 [DEBUG] Public schema ensured\n")
						}
						break
					}
					fmt.Printf("⚠️  [DEBUG] Ping failed: %v\n", err)
					if i == maxRetries-1 {
						fmt.Printf("❌ [DEBUG] Failed to connect after %d retries\n", maxRetries)
						return fmt.Errorf("failed to connect to frkrdb after creation: %w", err)
					}
					time.Sleep(1 * time.Second)
				}
			}
		} else {
			fmt.Printf("❌ [DEBUG] Database URL doesn't contain /frkrdb, returning original error\n")
			return err
		}
	} else {
		fmt.Printf("🔍 [DEBUG] Successfully connected to target database\n")
	}

	return nil
}

// BrokerChecker handles broker connection verification
type BrokerChecker struct{}

// NewBrokerChecker creates a new BrokerChecker
func NewBrokerChecker() *BrokerChecker {
	return &BrokerChecker{}
}

// Check verifies that the broker is accessible
func (bc *BrokerChecker) Check(brokerURL string) error {
	// Try to connect to the broker
	conn, err := kafka.Dial("tcp", brokerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to broker at %s: %w", brokerURL, err)
	}
	defer conn.Close()

	// Try to get broker metadata
	_, err = conn.Brokers()
	if err != nil {
		// If we can't get brokers, at least we connected, which is good enough
		return nil
	}

	return nil
}
