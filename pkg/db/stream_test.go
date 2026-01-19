package db

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	commonDB "github.com/frkr-io/frkr-common/db"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// setupTestDB is a convenience wrapper around the shared SetupTestDB from frkr-common
func setupTestDB(t *testing.T) (*sql.DB, string) {
	// Use the shared test utility from frkr-common
	return commonDB.SetupTestDB(t)
}

func TestCreateOrGetTenant(t *testing.T) {
	db, _ := setupTestDB(t)

	t.Run("create new tenant", func(t *testing.T) {
		tenant, err := CreateOrGetTenant(db, "test-tenant")
		require.NoError(t, err)
		require.NotEmpty(t, tenant.ID)
		require.Equal(t, "test-tenant", tenant.Name)
		require.Equal(t, "free", tenant.Plan)
		require.False(t, tenant.CreatedAt.IsZero())
	})

	t.Run("get existing tenant", func(t *testing.T) {
		// Create tenant first
		tenant1, err := CreateOrGetTenant(db, "existing-tenant")
		require.NoError(t, err)

		// Get it again
		tenant2, err := CreateOrGetTenant(db, "existing-tenant")
		require.NoError(t, err)

		// Should be the same tenant
		require.Equal(t, tenant1.ID, tenant2.ID)
		require.Equal(t, tenant1.Name, tenant2.Name)
	})
}

func TestCreateStream(t *testing.T) {
	db, _ := setupTestDB(t)

	// Create a tenant first
	tenant, err := CreateOrGetTenant(db, "stream-test-tenant")
	require.NoError(t, err)

	t.Run("create stream", func(t *testing.T) {
		stream, err := CreateStream(db, tenant.ID, "my-api", "Test API stream", 7)
		require.NoError(t, err)
		require.NotEmpty(t, stream.ID)
		require.Equal(t, tenant.ID, stream.TenantID)
		require.Equal(t, "my-api", stream.Name)
		require.Equal(t, "Test API stream", stream.Description)
		require.Equal(t, "active", stream.Status)
		require.Equal(t, 7, stream.RetentionDays)
		require.NotEmpty(t, stream.Topic)
		require.Contains(t, stream.Topic, "stream-")
		require.Contains(t, stream.Topic, "my-api")
	})

	t.Run("duplicate stream name fails", func(t *testing.T) {
		_, err := CreateStream(db, tenant.ID, "my-api", "Duplicate", 7)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})
}

func TestGetStream(t *testing.T) {
	db, _ := setupTestDB(t)

	tenant, err := CreateOrGetTenant(db, "get-stream-tenant")
	require.NoError(t, err)

	stream, err := CreateStream(db, tenant.ID, "test-stream", "Test", 7)
	require.NoError(t, err)

	t.Run("get stream by name", func(t *testing.T) {
		found, err := GetStream(db, tenant.ID, "test-stream")
		require.NoError(t, err)
		require.Equal(t, stream.ID, found.ID)
		require.Equal(t, stream.Name, found.Name)
	})

	t.Run("get stream by ID", func(t *testing.T) {
		found, err := GetStream(db, tenant.ID, stream.ID)
		require.NoError(t, err)
		require.Equal(t, stream.ID, found.ID)
		require.Equal(t, stream.Name, found.Name)
	})
}

func TestListStreams(t *testing.T) {
	db, _ := setupTestDB(t)

	tenant, err := CreateOrGetTenant(db, "list-streams-tenant")
	require.NoError(t, err)

	// Create multiple streams
	stream1, err := CreateStream(db, tenant.ID, "stream-1", "First", 7)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	stream2, err := CreateStream(db, tenant.ID, "stream-2", "Second", 14)
	require.NoError(t, err)

	t.Run("list all streams", func(t *testing.T) {
		streams, err := ListStreams(db, tenant.ID)
		require.NoError(t, err)
		require.Len(t, streams, 2)

		// Should be ordered by created_at DESC (newest first)
		require.Equal(t, stream2.ID, streams[0].ID)
		require.Equal(t, stream1.ID, streams[1].ID)
	})
}

func TestCreateOrGetTenant_EdgeCases(t *testing.T) {
	db, _ := setupTestDB(t)

	t.Run("empty tenant name fails", func(t *testing.T) {
		_, err := CreateOrGetTenant(db, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("very long tenant name fails", func(t *testing.T) {
		longName := strings.Repeat("a", 101)
		_, err := CreateOrGetTenant(db, longName)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed")
	})
}

func TestCreateStream_EdgeCases(t *testing.T) {
	db, _ := setupTestDB(t)

	tenant, err := CreateOrGetTenant(db, "edge-case-tenant")
	require.NoError(t, err)

	t.Run("empty stream name fails", func(t *testing.T) {
		_, err := CreateStream(db, tenant.ID, "", "Description", 7)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := CreateStream(db, "", "test-stream", "Description", 7)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("zero retention days fails", func(t *testing.T) {
		_, err := CreateStream(db, tenant.ID, "test-stream", "Description", 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be positive")
	})

	t.Run("negative retention days fails", func(t *testing.T) {
		_, err := CreateStream(db, tenant.ID, "test-stream", "Description", -1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be positive")
	})

	t.Run("retention days over 365 fails", func(t *testing.T) {
		_, err := CreateStream(db, tenant.ID, "test-stream", "Description", 366)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed 365")
	})

	t.Run("very long stream name fails", func(t *testing.T) {
		longName := strings.Repeat("a", 101)
		_, err := CreateStream(db, tenant.ID, longName, "Description", 7)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed")
	})

	t.Run("stream with special characters in name", func(t *testing.T) {
		stream, err := CreateStream(db, tenant.ID, "my-api_v2.0", "Test", 7)
		require.NoError(t, err)
		require.NotEmpty(t, stream.Topic)
		// Topic should be sanitized
		require.Contains(t, stream.Topic, "my-api-v2-0")
	})
}

func TestGetStream_EdgeCases(t *testing.T) {
	db, _ := setupTestDB(t)

	tenant, err := CreateOrGetTenant(db, "get-edge-tenant")
	require.NoError(t, err)

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := GetStream(db, "", "test-stream")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty stream identifier fails", func(t *testing.T) {
		_, err := GetStream(db, tenant.ID, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("non-existent stream fails", func(t *testing.T) {
		_, err := GetStream(db, tenant.ID, "non-existent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("stream from different tenant not found", func(t *testing.T) {
		otherTenant, err := CreateOrGetTenant(db, "other-tenant")
		require.NoError(t, err)

		stream, err := CreateStream(db, tenant.ID, "isolated-stream", "Test", 7)
		require.NoError(t, err)

		// Try to get stream with wrong tenant ID
		_, err = GetStream(db, otherTenant.ID, stream.ID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}

func TestListStreams_EdgeCases(t *testing.T) {
	db, _ := setupTestDB(t)

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := ListStreams(db, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("list streams for tenant with no streams", func(t *testing.T) {
		tenant, err := CreateOrGetTenant(db, "empty-tenant")
		require.NoError(t, err)

		streams, err := ListStreams(db, tenant.ID)
		require.NoError(t, err)
		require.Len(t, streams, 0)
	})
}
