package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	configFile = flag.String("config", "", "Path to YAML config file (non-interactive mode)")
)

func main() {
	flag.Parse()

	var config *FrkrupConfig
	var err error

	if *configFile != "" {
		config, err = loadConfigFromFile(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("📄 Loaded configuration from %s\n", *configFile)
	} else {
		config, err = promptConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// Apply defaults for fields not prompted
		applyDefaults(config)
	}

	// Ensure cleanup on exit (for local mode)
	if !config.K8s {
		cleanupMgr := NewCleanupManager(config)
		defer cleanupMgr.CleanupLocal()
		// Also register cleanup for signals to catch early exits
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			cleanupMgr.CleanupLocal()
			os.Exit(1)
		}()
	}

	if config.K8s {
		k8sMgr := NewKubernetesManager(config)
		if err := k8sMgr.Setup(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Kubernetes setup failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := setupLocal(config); err != nil {
			// Cleanup explicitly before exit (defer won't run on os.Exit)
			cleanupMgr := NewCleanupManager(config)
			cleanupMgr.CleanupLocal()
			fmt.Fprintf(os.Stderr, "❌ Local setup failed: %v\n", err)
			os.Exit(1)
		}
	}
}

// setupLocal performs the full local setup
func setupLocal(config *FrkrupConfig) error {
	fmt.Println("\n🚀 Setting up frkr locally...")

	// Setup infrastructure
	infraMgr := NewInfrastructureManager(config)
	if err := infraMgr.EnsureRunning(); err != nil {
		// Cleanup Docker if we started it
		if config.StartedDocker {
			cleanupMgr := NewCleanupManager(config)
			cleanupMgr.CleanupDocker()
		}
		return fmt.Errorf("infrastructure setup failed: %w", err)
	}

	// Build URLs
	dbURL := config.BuildDBURL()
	brokerURL := config.BuildBrokerURL()
	
	// Debug: show what URLs were built
	fmt.Printf("   [DEBUG] Config: DBHost=%s, DBPort=%s, BrokerHost=%s, BrokerPort=%s\n", 
		config.DBHost, config.DBPort, config.BrokerHost, config.BrokerPort)
	fmt.Printf("   [DEBUG] Migrations: %s\n", config.MigrationsPath)
	fmt.Printf("   [DEBUG] Built URLs: DB=%s, Broker=%s\n", dbURL, brokerURL)

	// Check database connection
	fmt.Println("\n🔍 Checking database connection...")
	dbChecker := NewDatabaseChecker()
	if err := dbChecker.Check(dbURL); err != nil {
		// Cleanup Docker if we started it
		if config.StartedDocker {
			cleanupMgr := NewCleanupManager(config)
			cleanupMgr.CleanupDocker()
		}
		return fmt.Errorf("database connection failed: %w", err)
	}
	fmt.Println("✅ Database connection successful")

	// Check broker connection
	fmt.Println("\n🔍 Checking broker connection...")
	brokerChecker := NewBrokerChecker()
	if err := brokerChecker.Check(brokerURL); err != nil {
		// Cleanup Docker if we started it
		if config.StartedDocker {
			cleanupMgr := NewCleanupManager(config)
			cleanupMgr.CleanupDocker()
		}
		return fmt.Errorf("broker connection failed: %w", err)
	}
	fmt.Println("✅ Broker connection successful")

	// Run migrations
	fmt.Println("\n🗄️  Running database migrations...")
	dbMgr := NewDatabaseManager(dbURL)
	if err := dbMgr.RunMigrations(config.MigrationsPath); err != nil {
		// Cleanup Docker if we started it
		if config.StartedDocker {
			cleanupMgr := NewCleanupManager(config)
			cleanupMgr.CleanupDocker()
		}
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println("✅ Migrations completed")

	// Create stream if requested
	if config.CreateStream {
		fmt.Printf("\n📡 Creating stream '%s'...\n", config.StreamName)
		stream, err := dbMgr.CreateStream(config.StreamName)
		if err != nil {
			fmt.Printf("⚠️  Stream creation failed (may already exist): %v\n", err)
		} else {
			fmt.Printf("✅ Stream '%s' created\n", config.StreamName)
			// Create topic for the stream (Kafka Protocol compliant)
			fmt.Printf("📦 Creating topic '%s'...\n", stream.Topic)
			brokerMgr := NewBrokerManager(brokerURL)
			if err := brokerMgr.CreateTopic(stream.Topic); err != nil {
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

	gatewayMgr := NewGatewaysManager(config)

	// Start ingest gateway
	fmt.Printf("   Starting ingest gateway on port %d...\n", config.IngestPort)
	ingestCmd, ingestStdout, ingestStderr := gatewayMgr.StartGateway(ctx, "ingest", config.IngestPort, dbURL, brokerURL)
	if ingestCmd == nil {
		return fmt.Errorf("failed to start ingest gateway")
	}
	config.IngestCmd = ingestCmd
	
	// Start streaming gateway
	fmt.Printf("   Starting streaming gateway on port %d...\n", config.StreamingPort)
	streamingCmd, streamingStdout, streamingStderr := gatewayMgr.StartGateway(ctx, "streaming", config.StreamingPort, dbURL, brokerURL)
	if streamingCmd == nil {
		cancel()
		killProcess(config.IngestCmd)
		config.IngestCmd = nil
		return fmt.Errorf("failed to start streaming gateway")
	}
	config.StreamingCmd = streamingCmd
	
	// Start reading stderr immediately to catch startup errors
	// Note: Gateways log to stderr, so these are normal logs, not necessarily errors
	go func() {
		scanner := bufio.NewScanner(ingestStderr)
		for scanner.Scan() {
			fmt.Printf("[INGEST] %s\n", scanner.Text())
		}
	}()
	go func() {
		scanner := bufio.NewScanner(streamingStderr)
		for scanner.Scan() {
			fmt.Printf("[STREAMING] %s\n", scanner.Text())
		}
	}()
	
	defer ingestStdout.Close()
	defer ingestStderr.Close()
	defer streamingStdout.Close()
	defer streamingStderr.Close()
	defer func() {
		if config.StreamingCmd != nil {
			killProcess(config.StreamingCmd)
			config.StreamingCmd = nil
		}
	}()

	// Wait for gateways to start (they now self-check their dependencies)
	fmt.Println("\n⏳ Waiting for gateways to be ready...")
	time.Sleep(5 * time.Second)

	// Verify gateways - they now report their own dependency status
	fmt.Println("\n🔍 Verifying gateways...")
	maxRetries := 10
	if err := gatewayMgr.VerifyGatewaysWithRetries(maxRetries); err != nil {
		cancel()
		killProcess(config.IngestCmd)
		killProcess(config.StreamingCmd)
		config.IngestCmd = nil
		config.StreamingCmd = nil
		return fmt.Errorf("gateway verification failed: %w", err)
	}

	fmt.Println("\n✅ frkr is running!")
	ingestHost := config.IngestHost
	if ingestHost == "" {
		ingestHost = "localhost"
	}
	streamingHost := config.StreamingHost
	if streamingHost == "" {
		streamingHost = "localhost"
	}
	fmt.Printf("   Ingest Gateway: http://%s:%d\n", ingestHost, config.IngestPort)
	fmt.Printf("   Streaming Gateway: http://%s:%d\n", streamingHost, config.StreamingPort)
	fmt.Println("\n📋 Gateway logs (Ctrl+C to stop):")

	// Stream logs
	gatewayMgr.StreamLogs(ingestStdout, ingestStderr, "INGEST")
	gatewayMgr.StreamLogs(streamingStdout, streamingStderr, "STREAMING")

	// Wait for context cancellation (from signal or error)
	<-ctx.Done()

	// Cleanup will be handled by defer, but we can also trigger it explicitly here
	// The cleanup manager will handle everything concurrently with timeouts
	fmt.Println("\n🛑 Shutting down...")
	return nil
}
