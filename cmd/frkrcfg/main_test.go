package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frkr-io/frkr-common/migrate"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/cockroachdb"
)

func setupTestDBForCLI(t *testing.T) (*sql.DB, string) {
	ctx := context.Background()

	// Start CockroachDB container
	cockroachContainer, err := cockroachdb.Run(ctx, "cockroachdb/cockroach:latest",
		cockroachdb.WithInsecure(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, cockroachContainer.Terminate(ctx))
	})

	// Get connection config
	connConfig, err := cockroachContainer.ConnectionConfig(ctx)
	require.NoError(t, err)

	// Get the mapped port
	port, err := cockroachContainer.MappedPort(ctx, "26257")
	require.NoError(t, err)

	// Build connection string for migrations (cockroachdb:// format)
	migrateURL := fmt.Sprintf("cockroachdb://%s@%s:%s/%s?sslmode=disable",
		connConfig.User,
		"localhost",
		port.Port(),
		connConfig.Database,
	)

	// Get absolute path to migrations directory
	migrationsPath, err := filepath.Abs("../../../frkr-common/migrations")
	require.NoError(t, err)

	// Run migrations
	err = migrate.RunMigrations(migrateURL, migrationsPath)
	require.NoError(t, err)

	// Build connection string for sql.Open (postgres:// format for lib/pq)
	dbURL := fmt.Sprintf("postgres://%s@localhost:%s/%s?sslmode=disable",
		connConfig.User,
		port.Port(),
		connConfig.Database,
	)

	// Open database connection
	db, err := sql.Open("postgres", dbURL)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})

	// Test connection
	err = db.Ping()
	require.NoError(t, err)

	return db, migrateURL
}

func TestStreamCreateCommand(t *testing.T) {
	_, dbURL := setupTestDBForCLI(t)

	t.Run("create stream successfully", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "create", "test-api",
			"--db-url", dbURL,
			"--tenant", "test-tenant",
			"--description", "Test API stream",
			"--retention-days", "7",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "✅ Stream created successfully")
		require.Contains(t, output, "test-api")
		require.Contains(t, output, "test-tenant")
	})

	t.Run("create stream with default retention", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "create", "default-retention-stream",
			"--db-url", dbURL,
			"--tenant", "test-tenant-2",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "✅ Stream created successfully")
		require.Contains(t, output, "default-retention-stream")
	})

	t.Run("create stream fails with invalid retention", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "create", "invalid-stream",
			"--db-url", dbURL,
			"--tenant", "test-tenant-3",
			"--retention-days", "400",
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
		require.Contains(t, buf.String(), "cannot exceed 365")
	})

	t.Run("create stream fails without db-url", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "create", "test-stream",
			"--tenant", "test-tenant",
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
		require.Contains(t, buf.String(), "required")
	})
}

func TestStreamListCommand(t *testing.T) {
	_, dbURL := setupTestDBForCLI(t)

	// Create a stream first
	rootCmd.SetArgs([]string{
		"stream", "create", "list-test-stream",
		"--db-url", dbURL,
		"--tenant", "list-tenant",
		"--description", "Stream for listing test",
	})
	rootCmd.SetOut(os.Stderr) // Suppress output
	err := rootCmd.Execute()
	require.NoError(t, err)

	t.Run("list streams successfully", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "list",
			"--db-url", dbURL,
			"--tenant", "list-tenant",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "list-tenant")
		require.Contains(t, output, "list-test-stream")
	})

	t.Run("list streams for empty tenant", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "list",
			"--db-url", dbURL,
			"--tenant", "empty-tenant",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "No streams found")
	})
}

func TestStreamGetCommand(t *testing.T) {
	_, dbURL := setupTestDBForCLI(t)

	// Create a stream first
	rootCmd.SetArgs([]string{
		"stream", "create", "get-test-stream",
		"--db-url", dbURL,
		"--tenant", "get-tenant",
		"--description", "Stream for get test",
	})
	rootCmd.SetOut(os.Stderr) // Suppress output
	err := rootCmd.Execute()
	require.NoError(t, err)

	t.Run("get stream by name successfully", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "get", "get-test-stream",
			"--db-url", dbURL,
			"--tenant", "get-tenant",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "Stream Details")
		require.Contains(t, output, "get-test-stream")
		require.Contains(t, output, "Stream for get test")
	})

	t.Run("get non-existent stream fails", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"stream", "get", "non-existent",
			"--db-url", dbURL,
			"--tenant", "get-tenant",
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
		require.Contains(t, buf.String(), "not found")
	})
}

func TestMigrateCommand(t *testing.T) {
	_, dbURL := setupTestDBForCLI(t)

	migrationsPath, err := filepath.Abs("../../../frkr-common/migrations")
	require.NoError(t, err)

	t.Run("migrate successfully", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"migrate",
			"--db-url", dbURL,
			"--migrations-path", migrationsPath,
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "✅ Migrations completed successfully")
	})

	t.Run("migrate fails without db-url", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"migrate",
			"--migrations-path", migrationsPath,
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
		require.Contains(t, buf.String(), "required")
	})
}

func TestUserCreateCommand(t *testing.T) {
	_, dbURL := setupTestDBForCLI(t)

	t.Run("create user successfully", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"user", "create", "testuser",
			"--db-url", dbURL,
			"--tenant", "user-tenant",
		})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "✅ User created successfully")
		require.Contains(t, output, "testuser")
		require.Contains(t, output, "user-tenant")
		require.Contains(t, output, "Password:")
	})

	t.Run("create user with invalid username fails", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"user", "create", "invalid@user",
			"--db-url", dbURL,
			"--tenant", "user-tenant",
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
		require.Contains(t, buf.String(), "alphanumeric")
	})

	t.Run("create user with empty username fails", func(t *testing.T) {
		rootCmd.SetArgs([]string{
			"user", "create", "",
			"--db-url", dbURL,
			"--tenant", "user-tenant",
		})

		var buf bytes.Buffer
		rootCmd.SetErr(&buf)

		err := rootCmd.Execute()
		require.Error(t, err)
	})
}

func TestRootCommand(t *testing.T) {
	t.Run("help command works", func(t *testing.T) {
		rootCmd.SetArgs([]string{"--help"})

		var outBuf, errBuf bytes.Buffer
		rootCmd.SetOut(&outBuf)
		rootCmd.SetErr(&errBuf)

		err := rootCmd.Execute()
		require.NoError(t, err)

		output := outBuf.String() + errBuf.String()
		require.Contains(t, output, "frkrcfg")
		require.Contains(t, output, "stream")
		require.Contains(t, output, "user")
		require.Contains(t, output, "migrate")
	})
}
