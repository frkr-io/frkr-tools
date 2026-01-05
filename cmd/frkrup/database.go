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
	fmt.Printf("🔍 [DEBUG] RunMigrations called with dbURL: %s\n", maskPassword(dm.dbURL))
	fmt.Printf("🔍 [DEBUG] migrationsPath: %s\n", migrationsPath)

	// migrationsPath should already be absolute, but ensure it is
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to resolve migrations path: %w", err)
	}
	fmt.Printf("🔍 [DEBUG] absolute migrations path: %s\n", absPath)

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found: %s", absPath)
	}
	fmt.Printf("🔍 [DEBUG] migrations directory exists\n")

	// Ensure database connection is valid before running migrations
	// This helps catch connection issues early
	fmt.Printf("🔍 [DEBUG] Opening database connection...\n")
	testDB, err := sql.Open("postgres", dm.dbURL)
	if err != nil {
		return fmt.Errorf("invalid database URL: %w", err)
	}
	defer testDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Printf("🔍 [DEBUG] Pinging database...\n")
	if err := testDB.PingContext(ctx); err != nil {
		return fmt.Errorf("cannot connect to database: %w", err)
	}
	fmt.Printf("🔍 [DEBUG] Database ping successful\n")

	// Check current database and schema
	fmt.Printf("🔍 [DEBUG] Checking current database...\n")
	var currentDB string
	if err := testDB.QueryRowContext(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
		fmt.Printf("⚠️  [DEBUG] Could not get current database: %v\n", err)
	} else {
		fmt.Printf("🔍 [DEBUG] Current database: %s\n", currentDB)
	}

	// Verify the database actually exists by connecting to defaultdb and listing
	fmt.Printf("🔍 [DEBUG] Verifying database exists by connecting to defaultdb...\n")
	defaultURL := strings.Replace(dm.dbURL, "/frkrdb", "/defaultdb", 1)
	defaultDB, err := sql.Open("postgres", defaultURL)
	if err == nil {
		defer defaultDB.Close()
		if err := defaultDB.PingContext(ctx); err == nil {
			rows, err := defaultDB.QueryContext(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false")
			if err == nil {
				defer rows.Close()
				fmt.Printf("🔍 [DEBUG] Databases that actually exist:\n")
				found := false
				for rows.Next() {
					var dbName string
					if err := rows.Scan(&dbName); err == nil {
						fmt.Printf("🔍 [DEBUG]   - %s\n", dbName)
						if dbName == "frkrdb" {
							found = true
						}
					}
				}
				if !found {
					fmt.Printf("❌ [DEBUG] frkrdb does NOT exist! Creating it now...\n")
					_, err = defaultDB.ExecContext(ctx, "CREATE DATABASE frkrdb")
					if err != nil {
						fmt.Printf("❌ [DEBUG] Failed to create database: %v\n", err)
					} else {
						fmt.Printf("🔍 [DEBUG] Database created, waiting for it to be ready...\n")
						time.Sleep(2 * time.Second)
						// Reconnect to frkrdb
						testDB.Close()
						testDB, err = sql.Open("postgres", dm.dbURL)
						if err != nil {
							return fmt.Errorf("failed to reconnect to frkrdb: %w", err)
						}
						if err := testDB.PingContext(ctx); err != nil {
							return fmt.Errorf("failed to ping frkrdb after creation: %w", err)
						}
						fmt.Printf("🔍 [DEBUG] Successfully reconnected to frkrdb\n")
					}
				} else {
					fmt.Printf("🔍 [DEBUG] frkrdb exists\n")
				}
			}
		}
	}

	// Check if public schema exists - use a simpler query that doesn't require information_schema
	fmt.Printf("🔍 [DEBUG] Checking for public schema using pg_namespace...\n")
	var schemaExists bool
	err = testDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_namespace 
			WHERE nspname = 'public'
		)
	`).Scan(&schemaExists)
	if err != nil {
		fmt.Printf("⚠️  [DEBUG] Could not check for public schema: %v\n", err)
		fmt.Printf("⚠️  [DEBUG] Error details: %+v\n", err)
	} else {
		fmt.Printf("🔍 [DEBUG] Public schema exists: %v\n", schemaExists)
	}

	// Ensure public schema exists (CockroachDB should create it automatically, but be explicit)
	fmt.Printf("🔍 [DEBUG] Ensuring public schema exists...\n")
	_, err = testDB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS public")
	if err != nil {
		fmt.Printf("⚠️  [DEBUG] Could not ensure public schema exists: %v\n", err)
	} else {
		fmt.Printf("🔍 [DEBUG] Public schema ensured\n")
	}

	// Use migrate package directly instead of calling frkrcfg as subprocess
	// This ensures the database drivers are properly linked
	// Detect if we're using CockroachDB by checking for pg_advisory_lock support
	// If it doesn't exist, use cockroachdb:// driver instead
	migrateURL := dm.dbURL
	fmt.Printf("🔍 [DEBUG] Checking database type...\n")
	var hasAdvisoryLock bool
	err = testDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'pg_advisory_lock')").Scan(&hasAdvisoryLock)
	if err != nil {
		fmt.Printf("⚠️  [DEBUG] Could not check for pg_advisory_lock: %v\n", err)
	} else {
		fmt.Printf("🔍 [DEBUG] Database has pg_advisory_lock: %v\n", hasAdvisoryLock)
		if !hasAdvisoryLock && strings.HasPrefix(migrateURL, "postgres://") {
			// This is likely CockroachDB - use cockroachdb:// driver
			migrateURL = strings.Replace(migrateURL, "postgres://", "cockroachdb://", 1)
			fmt.Printf("🔍 [DEBUG] Detected CockroachDB, using cockroachdb:// driver\n")
		}
	}
	fmt.Printf("🔍 [DEBUG] Calling migrate.RunMigrations with:\n")
	fmt.Printf("🔍 [DEBUG]   dbURL: %s\n", maskPassword(migrateURL))
	fmt.Printf("🔍 [DEBUG]   migrationsPath: %s\n", absPath)
	if err := migrate.RunMigrations(migrateURL, absPath); err != nil {
		fmt.Printf("❌ [DEBUG] migrate.RunMigrations failed: %v\n", err)
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	fmt.Printf("🔍 [DEBUG] Migrations completed successfully\n")
	return nil
}

// maskPassword masks the password in a database URL for logging
func maskPassword(dbURL string) string {
	// Simple masking - replace password with ***
	if strings.Contains(dbURL, "@") {
		parts := strings.Split(dbURL, "@")
		if len(parts) == 2 {
			// Check if there's a password (format: postgres://user:pass@host...)
			if strings.Contains(parts[0], ":") {
				userPass := strings.Split(parts[0], ":")
				if len(userPass) >= 3 {
					// postgres://user:pass format
					return strings.Join(userPass[:2], ":") + ":***@" + parts[1]
				}
			}
		}
	}
	return dbURL
}

// RunMigrationsK8s runs migrations for Kubernetes setup
func RunMigrationsK8s(repoRoot string) error {
	// Port forward database first
	cmd := exec.Command("kubectl", "port-forward", "svc/frkr-cockroachdb", "26257:26257")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to port forward database: %w", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(2 * time.Second)

	dbURL := "postgres://root@localhost:26257/frkrdb?sslmode=disable"
	migrationsPath, err := filepath.Abs(filepath.Join(repoRoot, "frkr-common", "migrations"))
	if err != nil {
		return err
	}

	dm := NewDatabaseManager(dbURL)
	return dm.RunMigrations(migrationsPath)
}

// CreateStream creates a stream in the database
func (dm *DatabaseManager) CreateStream(streamName string) (*models.Stream, error) {
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
		"--db-url", dm.dbURL,
		"--description", "Created by frkrup",
		"--retention-days", "7")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	dbConn, err := sql.Open("postgres", dm.dbURL)
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
