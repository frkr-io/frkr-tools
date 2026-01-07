package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dbcommon "github.com/frkr-io/frkr-common/db"
	"github.com/frkr-io/frkr-common/migrate"
	"github.com/frkr-io/frkr-common/models"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/lib/pq"
)

// DatabaseManager handles database operations
type DatabaseManager struct {
	dbURL string
}

// NewDatabaseManager creates a new DatabaseManager
func NewDatabaseManager(dbURL string) *DatabaseManager {
	return &DatabaseManager{dbURL: dbURL}
}

// RunMigrations runs database migrations
func (dm *DatabaseManager) RunMigrations(migrationsPath string) error {
	// Resolve migrations path
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found: %s", absPath)
	}

	// Test database connection
	testDB, err := sql.Open("postgres", dm.dbURL)
	if err != nil {
		return fmt.Errorf("invalid database URL: %w", err)
	}
	defer testDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := testDB.PingContext(ctx); err != nil {
		return fmt.Errorf("cannot connect to database: %w", err)
	}

	// Ensure database exists
	if err := dm.ensureDatabaseExists(testDB, ctx); err != nil {
		return err
	}

	// Ensure public schema exists
	_, _ = testDB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS public")

	// Detect database type and run migrations
	migrateURL := dm.dbURL
	var hasAdvisoryLock bool
	err = testDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'pg_advisory_lock')").Scan(&hasAdvisoryLock)
	if err == nil && !hasAdvisoryLock && strings.HasPrefix(migrateURL, "postgres://") {
		// CockroachDB doesn't support pg_advisory_lock - use cockroachdb:// driver
		migrateURL = strings.Replace(migrateURL, "postgres://", "cockroachdb://", 1)
	}

	if err := migrate.RunMigrations(migrateURL, absPath); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// ensureDatabaseExists creates the database if it doesn't exist
func (dm *DatabaseManager) ensureDatabaseExists(testDB *sql.DB, ctx context.Context) error {
	// Connect to defaultdb to check/create frkrdb
	defaultURL := strings.Replace(dm.dbURL, "/frkrdb", "/defaultdb", 1)
	defaultDB, err := sql.Open("postgres", defaultURL)
	if err != nil {
		return nil // Can't verify, proceed anyway
	}
	defer defaultDB.Close()

	if err := defaultDB.PingContext(ctx); err != nil {
		return nil // Can't verify, proceed anyway
	}

	rows, err := defaultDB.QueryContext(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false")
	if err != nil {
		return nil // Can't verify, proceed anyway
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err == nil {
			if dbName == "frkrdb" {
				found = true
				break
			}
		}
	}

	if !found {
		fmt.Println("   Creating database frkrdb...")
		_, err = defaultDB.ExecContext(ctx, "CREATE DATABASE frkrdb")
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	return nil
}

// maskPassword masks the password in a database URL for logging
func maskPassword(dbURL string) string {
	if strings.Contains(dbURL, "@") {
		parts := strings.Split(dbURL, "@")
		if len(parts) == 2 {
			if strings.Contains(parts[0], ":") {
				userPass := strings.Split(parts[0], ":")
				if len(userPass) >= 3 {
					return strings.Join(userPass[:2], ":") + ":***@" + parts[1]
				}
			}
		}
	}
	return dbURL
}

// RunMigrationsK8s runs migrations for Kubernetes setup
// RunMigrationsK8s runs migrations for Kubernetes setup using a temporary port-forward
func RunMigrationsK8s(migrationsPath string) error {
	cmd := exec.Command("kubectl", "port-forward", "svc/frkr-cockroachdb", "26257:26257")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to port forward database: %w", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(3 * time.Second)

	dbURL := "postgres://root@localhost:26257/frkrdb?sslmode=disable"
	dm := NewDatabaseManager(dbURL)
	return dm.RunMigrations(migrationsPath)
}

// CreateStream creates a stream in the database directly using the db library
func (dm *DatabaseManager) CreateStream(streamName string) (*models.Stream, error) {
	dbConn, err := sql.Open("postgres", dm.dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer dbConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test database connection and ensure database exists
	// If the database doesn't exist, PingContext might fail on some drivers
	// but for CockroachDB we need to connect to defaultdb first if we're not sure
	if err := dm.ensureDatabaseExists(dbConn, ctx); err != nil {
		return nil, err
	}

	if err := dbConn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}

	// Create or get tenant
	tenant, err := dbcommon.CreateOrGetTenant(dbConn, "default")
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	// Create stream using the common library
	// We use 7 days retention as default
	stream, err := dbcommon.CreateStream(dbConn, tenant.ID, streamName, "Created by frkrup", 7)
	if err != nil {
		// If it already exists, that's fine, we want to return the existing one
		if strings.Contains(err.Error(), "already exists") {
			return dbcommon.GetStream(dbConn, tenant.ID, streamName)
		}
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return stream, nil
}
