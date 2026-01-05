package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "frkrcfg",
	Short: "frkrcfg - Direct configuration tool for local development",
	Long:  `frkrcfg provides direct database access for local development without requiring Kubernetes.`,
}

var (
	dbURL      string
	tenantName string
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&dbURL, "db-url", "", "Database connection URL (required)")
	rootCmd.PersistentFlags().StringVar(&tenantName, "tenant", "default", "Tenant name (default: 'default')")

	rootCmd.AddCommand(streamCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(migrateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getDB() (*sql.DB, error) {
	if dbURL == "" {
		return nil, fmt.Errorf("--db-url is required")
	}

	// Convert cockroachdb:// to postgres:// for lib/pq
	connStr := dbURL
	if strings.HasPrefix(dbURL, "cockroachdb://") {
		connStr = strings.Replace(dbURL, "cockroachdb://", "postgres://", 1)
	}

	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Set connection pool settings
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(0) // No limit

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return conn, nil
}
