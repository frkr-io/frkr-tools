package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	dbcommon "github.com/frkr-io/frkr-common/db"
	"github.com/frkr-io/frkr-common/models"
	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"
)

type Config struct {
	K8s            bool
	K8sClusterName string
	DBHost         string
	DBPort         string
	DBUser         string
	DBPassword     string
	DBName         string
	BrokerHost     string
	BrokerPort     string
	BrokerUser     string
	BrokerPassword string
	IngestPort     int
	StreamingPort  int
	MigrationsPath string
	StreamName     string
	CreateStream   bool
	StartedDocker  bool // Track if we started Docker Compose
}

func main() {
	config, err := promptConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Ensure cleanup on exit (for local mode)
	if !config.K8s {
		defer cleanupLocal(config)
	}

	if config.K8s {
		if err := setupK8s(config); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Kubernetes setup failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := setupLocal(config); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Local setup failed: %v\n", err)
			os.Exit(1)
		}
	}
}

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

	fmt.Print("Database name [defaultdb]: ")
	scanner.Scan()
	config.DBName = strings.TrimSpace(scanner.Text())
	if config.DBName == "" {
		config.DBName = "defaultdb"
	}

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

func setupK8s(config *Config) error {
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
	if config.K8sClusterName == "" {
		// Try to get cluster name from kubectl context
		ctxCmd := exec.Command("kubectl", "config", "current-context")
		ctxOutput, err := ctxCmd.Output()
		if err == nil {
			ctxStr := strings.TrimSpace(string(ctxOutput))
			// For kind clusters, context is usually "kind-<cluster-name>"
			if strings.HasPrefix(ctxStr, "kind-") {
				config.K8sClusterName = strings.TrimPrefix(ctxStr, "kind-")
			} else {
				// Prompt for cluster name
				fmt.Print("Kubernetes cluster name: ")
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				config.K8sClusterName = strings.TrimSpace(scanner.Text())
				if config.K8sClusterName == "" {
					return fmt.Errorf("cluster name is required")
				}
			}
		} else {
			// Prompt for cluster name
			fmt.Print("Kubernetes cluster name: ")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			config.K8sClusterName = strings.TrimSpace(scanner.Text())
			if config.K8sClusterName == "" {
				return fmt.Errorf("cluster name is required")
			}
		}
	}
	fmt.Printf("Using cluster: %s\n", config.K8sClusterName)

	// Get repo root (assume we're in frkr-tools)
	repoRoot, err := filepath.Abs("../")
	if err != nil {
		return fmt.Errorf("failed to get repo root: %w", err)
	}

	// Build and load gateway images
	fmt.Println("\n📦 Building and loading gateway images...")

	ingestGatewayPath, err := findGatewayRepoPath("ingest")
	if err != nil {
		return fmt.Errorf("failed to find ingest gateway: %w", err)
	}
	if err := buildAndLoadImage(config, ingestGatewayPath, "frkr-ingest-gateway:0.1.0"); err != nil {
		return fmt.Errorf("failed to build ingest gateway: %w", err)
	}

	streamingGatewayPath, err := findGatewayRepoPath("streaming")
	if err != nil {
		return fmt.Errorf("failed to find streaming gateway: %w", err)
	}
	if err := buildAndLoadImage(config, streamingGatewayPath, "frkr-streaming-gateway:0.1.0"); err != nil {
		return fmt.Errorf("failed to build streaming gateway: %w", err)
	}

	// Install helm chart
	fmt.Println("\n📥 Installing frkr Helm chart...")
	helmPath, err := findInfraRepoPath("helm")
	if err != nil {
		return fmt.Errorf("failed to find frkr-infra-helm: %w", err)
	}
	helmCmd := exec.Command("helm", "install", "frkr", ".", "-f", "values-full.yaml")
	helmCmd.Dir = helmPath
	helmCmd.Stdout = os.Stdout
	helmCmd.Stderr = os.Stderr
	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm install failed: %w", err)
	}

	// Wait for pods
	fmt.Println("\n⏳ Waiting for pods to be ready...")
	waitPods := []string{
		"app.kubernetes.io/component=operator",
		"app.kubernetes.io/component=ingest-gateway",
		"app.kubernetes.io/component=streaming-gateway",
	}

	for _, selector := range waitPods {
		cmd := exec.Command("kubectl", "wait", "--for=condition=ready", "pod", "-l", selector, "--timeout=300s")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pod not ready: %s", selector)
		}
	}

	// Port forward
	fmt.Println("\n🔌 Setting up port forwarding...")
	portForwards := []struct {
		service string
		local   string
		remote  string
	}{
		{"frkr-ingest-gateway", fmt.Sprintf("%d", config.IngestPort), "8080"},
		{"frkr-streaming-gateway", fmt.Sprintf("%d", config.StreamingPort), "8081"},
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
			return fmt.Errorf("port forward failed for %s: %w", pf.service, err)
		}
		portForwardCmds = append(portForwardCmds, cmd)
		fmt.Printf("✅ Port forwarding %s:%s -> %s\n", pf.local, pf.remote, pf.service)
	}

	// Run migrations
	fmt.Println("\n🗄️  Running database migrations...")
	if err := runMigrationsK8s(repoRoot); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	// Verify gateways
	fmt.Println("\n✅ Verifying gateways...")
	time.Sleep(2 * time.Second)
	if err := verifyGateways(config.IngestPort, config.StreamingPort); err != nil {
		return fmt.Errorf("gateway verification failed: %w", err)
	}

	fmt.Println("\n✅ frkr is running on Kubernetes!")
	fmt.Printf("   Ingest Gateway: http://localhost:%d\n", config.IngestPort)
	fmt.Printf("   Streaming Gateway: http://localhost:%d\n", config.StreamingPort)
	fmt.Println("\nPress Ctrl+C to stop port forwarding and exit.")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n🛑 Stopping port forwarding...")
	for _, cmd := range portForwardCmds {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
	return nil
}

func setupLocal(config *Config) error {
	fmt.Println("\n🚀 Setting up frkr locally...")

	// Check if services are running, offer to start Docker Compose if not
	if err := ensureInfrastructureRunning(config); err != nil {
		return fmt.Errorf("infrastructure setup failed: %w", err)
	}

	// Build DB URL
	dbURL := buildDBURL(config)
	brokerURL := buildBrokerURL(config)

	// Check database connection
	fmt.Println("\n🔍 Checking database connection...")
	if err := checkDatabase(dbURL); err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	fmt.Println("✅ Database connection successful")

	// Check broker connection
	fmt.Println("\n🔍 Checking broker connection...")
	if err := checkBroker(brokerURL); err != nil {
		return fmt.Errorf("broker connection failed: %w", err)
	}
	fmt.Println("✅ Broker connection successful")

	// Run migrations
	fmt.Println("\n🗄️  Running database migrations...")
	if err := runMigrations(dbURL, config.MigrationsPath); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println("✅ Migrations completed")

	// Create stream if requested
	if config.CreateStream {
		fmt.Printf("\n📡 Creating stream '%s'...\n", config.StreamName)
		stream, err := createStream(dbURL, config.StreamName)
		if err != nil {
			fmt.Printf("⚠️  Stream creation failed (may already exist): %v\n", err)
		} else {
			fmt.Printf("✅ Stream '%s' created\n", config.StreamName)
			// Create topic for the stream (Kafka Protocol compliant)
			fmt.Printf("📦 Creating topic '%s'...\n", stream.Topic)
			if err := createTopic(brokerURL, stream.Topic); err != nil {
				fmt.Printf("⚠️  Topic creation failed (may already exist): %v\n", err)
			} else {
				fmt.Printf("✅ Topic '%s' created\n", stream.Topic)
			}
		}
	}

	// Start gateways
	fmt.Println("\n🚀 Starting gateways...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n🛑 Shutting down gateways...")
		cancel()
	}()

	// Start ingest gateway
	ingestCmd, ingestStdout, ingestStderr := startGateway(ctx, "ingest", config.IngestPort, dbURL, brokerURL)
	if ingestCmd == nil {
		return fmt.Errorf("failed to start ingest gateway")
	}
	defer ingestStdout.Close()
	defer ingestStderr.Close()

	// Start streaming gateway
	streamingCmd, streamingStdout, streamingStderr := startGateway(ctx, "streaming", config.StreamingPort, dbURL, brokerURL)
	if streamingCmd == nil {
		cancel()
		killProcess(ingestCmd)
		return fmt.Errorf("failed to start streaming gateway")
	}
	defer streamingStdout.Close()
	defer streamingStderr.Close()

	// Wait a bit for gateways to start
	fmt.Println("\n⏳ Waiting for gateways to start...")
	time.Sleep(5 * time.Second)

	// Verify gateways with retries
	fmt.Println("\n✅ Verifying gateways...")
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if err := verifyGateways(config.IngestPort, config.StreamingPort); err != nil {
			if i < maxRetries-1 {
				fmt.Printf("  Retrying... (%d/%d)\n", i+1, maxRetries)
				time.Sleep(2 * time.Second)
				continue
			}
			cancel()
			killProcess(ingestCmd)
			killProcess(streamingCmd)
			return fmt.Errorf("gateway verification failed: %w", err)
		}
		break
	}

	fmt.Println("\n✅ frkr is running!")
	fmt.Printf("   Ingest Gateway: http://localhost:%d\n", config.IngestPort)
	fmt.Printf("   Streaming Gateway: http://localhost:%d\n", config.StreamingPort)
	fmt.Println("\n📋 Gateway logs (Ctrl+C to stop):")

	// Stream logs
	streamLogs(ingestStdout, ingestStderr, "INGEST")
	streamLogs(streamingStdout, streamingStderr, "STREAMING")

	// Wait for context cancellation (from signal or error)
	<-ctx.Done()

	// Kill processes explicitly (CommandContext should handle this, but be explicit)
	fmt.Println("\n🛑 Stopping gateways...")
	killProcess(ingestCmd)
	killProcess(streamingCmd)

	// Wait a moment to ensure processes are fully terminated
	time.Sleep(500 * time.Millisecond)

	// Double-check and force kill any remaining processes
	if ingestCmd != nil && ingestCmd.Process != nil {
		ingestCmd.Process.Kill()
		ingestCmd.Wait()
	}
	if streamingCmd != nil && streamingCmd.Process != nil {
		streamingCmd.Process.Kill()
		streamingCmd.Wait()
	}

	fmt.Println("✅ Gateways stopped")
	return nil
}

func buildDBURL(config *Config) string {
	var parts []string
	if config.DBUser != "" {
		if config.DBPassword != "" {
			parts = append(parts, fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				config.DBUser, config.DBPassword, config.DBHost, config.DBPort, config.DBName))
		} else {
			parts = append(parts, fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
				config.DBUser, config.DBHost, config.DBPort, config.DBName))
		}
	} else {
		parts = append(parts, fmt.Sprintf("postgres://%s:%s/%s?sslmode=disable",
			config.DBHost, config.DBPort, config.DBName))
	}
	return parts[0]
}

func buildBrokerURL(config *Config) string {
	return fmt.Sprintf("%s:%s", config.BrokerHost, config.BrokerPort)
}

// ensureInfrastructureRunning checks if database and broker are running,
// and optionally starts Docker Compose if they're not available.
func ensureInfrastructureRunning(config *Config) error {
	// Quick check if services are already running
	dbRunning := isPortOpen(config.DBHost, config.DBPort)
	brokerRunning := isPortOpen(config.BrokerHost, config.BrokerPort)

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
			config.DBHost, config.DBPort, config.BrokerHost, config.BrokerPort)
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

	config.StartedDocker = true

	// Wait for services to be ready
	fmt.Println("\n⏳ Waiting for services to be ready...")
	maxWait := 60 // seconds
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if isPortOpen(config.DBHost, config.DBPort) && isPortOpen(config.BrokerHost, config.BrokerPort) {
			fmt.Println("✅ Services are ready")
			return nil
		}
		if (i+1)%10 == 0 {
			fmt.Printf("   Still waiting... (%d/%d seconds)\n", i+1, maxWait)
		}
	}

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

func checkDatabase(dbURL string) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	return nil
}

func checkBroker(brokerURL string) error {
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

func runMigrations(dbURL, migrationsPath string) error {
	// migrationsPath should already be absolute, but ensure it is
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found: %s", absPath)
	}

	// Use frkrcfg migrate
	repoRoot, err := filepath.Abs("../")
	if err != nil {
		return err
	}

	frkrcfgPath := filepath.Join(repoRoot, "frkr-tools", "bin", "frkrcfg")
	if _, err := os.Stat(frkrcfgPath); os.IsNotExist(err) {
		// Build it
		cmd := exec.Command("go", "run", "build.go")
		cmd.Dir = filepath.Join(repoRoot, "frkr-tools")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build frkrcfg: %w", err)
		}
	}

	cmd := exec.Command(frkrcfgPath, "migrate", "--db-url", dbURL, "--migrations-path", absPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runMigrationsK8s(repoRoot string) error {
	// Port forward database first
	cmd := exec.Command("kubectl", "port-forward", "svc/frkr-cockroachdb", "26257:26257")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to port forward database: %w", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(2 * time.Second)

	dbURL := "cockroachdb://root@localhost:26257/defaultdb?sslmode=disable"
	migrationsPath, err := filepath.Abs(filepath.Join(repoRoot, "frkr-common", "migrations"))
	if err != nil {
		return err
	}

	return runMigrations(dbURL, migrationsPath)
}

func createStream(dbURL, streamName string) (*models.Stream, error) {
	repoRoot, err := filepath.Abs("../")
	if err != nil {
		return nil, err
	}

	frkrcfgPath := filepath.Join(repoRoot, "frkr-tools", "bin", "frkrcfg")
	if _, err := os.Stat(frkrcfgPath); os.IsNotExist(err) {
		// Build it
		cmd := exec.Command("go", "run", "build.go")
		cmd.Dir = filepath.Join(repoRoot, "frkr-tools")
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to build frkrcfg: %w", err)
		}
	}

	cmd := exec.Command(frkrcfgPath, "stream", "create", streamName,
		"--db-url", dbURL,
		"--description", "Created by frkrup",
		"--retention-days", "7")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// Get the created stream to return topic name
	// We'll query it from the database
	connStr := dbURL
	if strings.HasPrefix(dbURL, "cockroachdb://") {
		connStr = strings.Replace(dbURL, "cockroachdb://", "postgres://", 1)
	}

	dbConn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer dbConn.Close()

	tenant, err := dbcommon.CreateOrGetTenant(dbConn, "default")
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	stream, err := dbcommon.GetStream(dbConn, tenant.ID, streamName)
	if err != nil {
		return nil, fmt.Errorf("failed to get created stream: %w", err)
	}

	return stream, nil
}

// createTopic creates a topic for a stream (Kafka Protocol compliant).
// Security: This function is only called after:
// 1. Stream has been created in the database (validated)
// 2. Topic name comes from the database (not user input)
func createTopic(brokerURL, topicName string) error {
	conn, err := kafka.Dial("tcp", brokerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topicName,
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		// Topic might already exist, which is fine
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("failed to create topic: %w", err)
	}

	return nil
}

func startGateway(ctx context.Context, gatewayType string, port int, dbURL, brokerURL string) (*exec.Cmd, io.ReadCloser, io.ReadCloser) {
	gatewayPath, err := findGatewayPath(gatewayType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to find gateway: %v\n", err)
		return nil, nil, nil
	}

	// Get the directory containing the gateway main.go
	gatewayDir := gatewayPath
	mainFile := filepath.Join(gatewayDir, "main.go")

	cmd := exec.CommandContext(ctx, "go", "run", mainFile)
	cmd.Dir = gatewayDir
	cmd.Args = append(cmd.Args,
		"--http-port", fmt.Sprintf("%d", port),
		"--db-url", dbURL,
		"--broker-url", brokerURL)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return nil, nil, nil
	}

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, nil, nil
	}

	return cmd, stdout, stderr
}

func streamLogs(stdout, stderr io.ReadCloser, label string) {
	// Stream stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Printf("[%s] %s\n", label, scanner.Text())
		}
	}()

	// Stream stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", label, scanner.Text())
		}
	}()
}

func verifyGateways(ingestPort, streamingPort int) error {
	// Check ingest gateway
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", ingestPort))
	if err != nil {
		return fmt.Errorf("ingest gateway health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest gateway returned status %d", resp.StatusCode)
	}

	// Check streaming gateway
	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/health", streamingPort))
	if err != nil {
		return fmt.Errorf("streaming gateway health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("streaming gateway returned status %d", resp.StatusCode)
	}

	return nil
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	fmt.Printf("🛑 Killing process %d...\n", cmd.Process.Pid)

	// Try graceful shutdown first (SIGTERM)
	cmd.Process.Signal(syscall.SIGTERM)

	// Wait a bit for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully
		return
	case <-time.After(2 * time.Second):
		// Force kill if still running
		cmd.Process.Kill()
		cmd.Wait()
	}
}

func cleanupLocal(config *Config) {
	fmt.Println("\n🧹 Cleaning up...")

	// Stop Docker Compose if we started it
	if config.StartedDocker {
		fmt.Println("🛑 Stopping Docker Compose services...")
		dockerPath, err := findInfraRepoPath("docker")
		if err == nil {
			cmd := exec.Command("docker", "compose", "down")
			cmd.Dir = dockerPath
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run() // Ignore errors - services might already be stopped
		}
	}

	// Find and kill any remaining gateway processes
	fmt.Println("🛑 Stopping gateway processes...")
	// Look for "go run" processes that match gateway patterns
	cmd := exec.Command("pkill", "-f", "go run.*gateway")
	cmd.Run() // Ignore errors - process might not exist

	// Also try to kill any processes on the gateway ports
	// This is a fallback in case processes weren't properly tracked
	cmd = exec.Command("lsof", "-ti", fmt.Sprintf(":%d", config.IngestPort))
	if output, err := cmd.Output(); err == nil {
		if pid := strings.TrimSpace(string(output)); pid != "" {
			exec.Command("kill", "-9", pid).Run()
		}
	}

	cmd = exec.Command("lsof", "-ti", fmt.Sprintf(":%d", config.StreamingPort))
	if output, err := cmd.Output(); err == nil {
		if pid := strings.TrimSpace(string(output)); pid != "" {
			exec.Command("kill", "-9", pid).Run()
		}
	}

	fmt.Println("✅ Cleanup complete")
}

func buildAndLoadImage(config *Config, path, imageName string) error {
	// Check for Dockerfile
	dockerfile := filepath.Join(path, "Dockerfile")
	if _, err := os.Stat(dockerfile); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found at %s", dockerfile)
	}

	// Build image
	fmt.Printf("  Building %s...\n", imageName)
	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfile, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Load into kind cluster
	fmt.Printf("  Loading %s into kind cluster '%s'...\n", imageName, config.K8sClusterName)
	cmd = exec.Command("kind", "load", "docker-image", imageName, "--name", config.K8sClusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind load failed (make sure kind cluster exists): %w", err)
	}

	return nil
}

// findGatewayRepoPath finds the gateway repository root path using git submodules.
// It automatically initializes submodules if needed.
func findGatewayRepoPath(gatewayType string) (string, error) {
	// Get frkr-tools directory (where we are running from)
	frkrupDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Find frkr-tools root by looking for .gitmodules
	toolsRoot := frkrupDir
	for {
		gitmodulesPath := filepath.Join(toolsRoot, ".gitmodules")
		if _, err := os.Stat(gitmodulesPath); err == nil {
			break
		}
		parent := filepath.Dir(toolsRoot)
		if parent == toolsRoot {
			return "", fmt.Errorf("frkr-tools root not found (no .gitmodules)")
		}
		toolsRoot = parent
	}

	var gatewayName string
	if gatewayType == "ingest" {
		gatewayName = "frkr-ingest-gateway"
	} else if gatewayType == "streaming" {
		gatewayName = "frkr-streaming-gateway"
	} else {
		return "", fmt.Errorf("unknown gateway type: %s", gatewayType)
	}

	submodulePath := filepath.Join(toolsRoot, gatewayName)
	goModPath := filepath.Join(submodulePath, "go.mod")

	// Check if submodule exists and is initialized
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		// Try to initialize submodule
		fmt.Printf("  Initializing submodule %s...\n", gatewayName)
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", submodulePath)
		cmd.Dir = toolsRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to initialize submodule %s: %w\n\nHint: Make sure you cloned frkr-tools with --recurse-submodules, or run: git submodule update --init --recursive", gatewayName, err)
		}
	}

	// Verify submodule is now available
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return "", fmt.Errorf("gateway %s not found at %s\n\nHint: Run 'git submodule update --init --recursive' in frkr-tools", gatewayName, submodulePath)
	}

	return submodulePath, nil
}

// findGatewayPath finds the gateway cmd directory path using git submodules.
// It automatically initializes submodules if needed.
func findGatewayPath(gatewayType string) (string, error) {
	repoPath, err := findGatewayRepoPath(gatewayType)
	if err != nil {
		return "", err
	}

	// Return path to cmd/gateway directory
	cmdPath := filepath.Join(repoPath, "cmd", "gateway")
	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		return "", fmt.Errorf("gateway cmd directory not found at %s", cmdPath)
	}

	return cmdPath, nil
}

// findInfraRepoPath finds the infrastructure repository path using git submodules.
// It automatically initializes submodules if needed.
func findInfraRepoPath(infraType string) (string, error) {
	// Get frkr-tools directory (where we are running from)
	frkrupDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Find frkr-tools root by looking for .gitmodules
	toolsRoot := frkrupDir
	for {
		gitmodulesPath := filepath.Join(toolsRoot, ".gitmodules")
		if _, err := os.Stat(gitmodulesPath); err == nil {
			break
		}
		parent := filepath.Dir(toolsRoot)
		if parent == toolsRoot {
			return "", fmt.Errorf("frkr-tools root not found (no .gitmodules)")
		}
		toolsRoot = parent
	}

	var repoName string
	switch infraType {
	case "helm":
		repoName = "frkr-infra-helm"
	case "docker":
		repoName = "frkr-infra-docker"
	default:
		return "", fmt.Errorf("unknown infra type: %s", infraType)
	}

	submodulePath := filepath.Join(toolsRoot, repoName)

	// Check if submodule exists and is initialized
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		// Try to initialize submodule
		fmt.Printf("  Initializing submodule %s...\n", repoName)
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", submodulePath)
		cmd.Dir = toolsRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to initialize submodule %s: %w\n\nHint: Make sure you cloned frkr-tools with --recurse-submodules, or run: git submodule update --init --recursive", repoName, err)
		}
	}

	// Verify submodule is now available
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		return "", fmt.Errorf("infrastructure repository %s not found at %s\n\nHint: Run 'git submodule update --init --recursive' in frkr-tools", repoName, submodulePath)
	}

	return submodulePath, nil
}

// findMigrationsPath uses Go module resolution to find frkr-common/migrations
// Works with both local (replace directive) and remote dependencies
func findMigrationsPath() (string, error) {
	// Use Go's module resolution - respects replace directives and module cache
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/frkr-io/frkr-common")
	output, err := cmd.Output()
	if err == nil {
		moduleDir := strings.TrimSpace(string(output))
		migrationsPath := filepath.Join(moduleDir, "migrations")
		if _, err := os.Stat(migrationsPath); err == nil {
			return migrationsPath, nil
		}
	}

	// Fallback: try local path (for development when go.mod isn't set up yet)
	repoRoot, _ := filepath.Abs("../")
	localPath := filepath.Join(repoRoot, "frkr-common", "migrations")
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	return "", fmt.Errorf("migrations not found: frkr-common module not found via 'go list -m' and local path %s does not exist", localPath)
}
