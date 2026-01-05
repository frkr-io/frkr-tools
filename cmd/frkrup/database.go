package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// migrationsPath should already be absolute, but ensure it is
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	// Verify migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found: %s", absPath)
	}

	// Ensure database connection is valid before running migrations
	// This helps catch connection issues early
	testDB, err := sql.Open("postgres", dm.dbURL)
	if err != nil {
		return fmt.Errorf("invalid database URL: %w", err)
	}
	defer testDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := testDB.PingContext(ctx); err != nil {
		return fmt.Errorf("cannot connect to database: %w", err)
	}

	// Use migrate package directly instead of calling frkrcfg as subprocess
	// This ensures the database drivers are properly linked
	if err := migrate.RunMigrations(dm.dbURL, absPath); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
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
