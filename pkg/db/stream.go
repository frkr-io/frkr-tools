package db

import (
	"database/sql"

	commondb "github.com/frkr-io/frkr-common/db"
	"github.com/frkr-io/frkr-common/models"
)

// CreateOrGetTenant creates a tenant or returns existing one
func CreateOrGetTenant(db *sql.DB, name string) (*models.Tenant, error) {
	return commondb.CreateOrGetTenant(db, name)
}

// CreateStream creates a new stream for a tenant
func CreateStream(db *sql.DB, tenantID, streamName, description string, retentionDays int) (*models.Stream, error) {
	return commondb.CreateStream(db, tenantID, streamName, description, retentionDays)
}

// GetStream retrieves a stream by ID or name
func GetStream(db *sql.DB, tenantID, streamIdentifier string) (*models.Stream, error) {
	return commondb.GetStream(db, tenantID, streamIdentifier)
}

// ListStreams lists all streams for a tenant
func ListStreams(db *sql.DB, tenantID string) ([]*models.Stream, error) {
	return commondb.ListStreams(db, tenantID)
}

// DeleteStream soft-deletes a stream by setting deleted_at
func DeleteStream(db *sql.DB, tenantID, streamIdentifier string) error {
	// First, verify the stream exists
	stream, err := GetStream(db, tenantID, streamIdentifier)
	if err != nil {
		return err
	}

	// Soft delete by setting deleted_at
	_, err = db.Exec(`
		UPDATE streams
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, stream.ID, tenantID)

	return err
}
