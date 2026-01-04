package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/frkr-io/frkr-common/models"
	"github.com/lib/pq"
)

// CreateOrGetTenant creates a tenant or returns existing one
func CreateOrGetTenant(db *sql.DB, name string) (*models.Tenant, error) {
	if name == "" {
		return nil, fmt.Errorf("tenant name cannot be empty")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("tenant name cannot exceed 100 characters")
	}

	var tenant models.Tenant

	// Try to get existing tenant
	err := db.QueryRow(`
		SELECT id, name, plan, created_at, updated_at, deleted_at
		FROM tenants
		WHERE name = $1 AND deleted_at IS NULL
	`, name).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Plan,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DeletedAt,
	)

	if err == nil {
		return &tenant, nil
	}

	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to query tenant: %w", err)
	}

	// Create new tenant
	err = db.QueryRow(`
		INSERT INTO tenants (name, plan)
		VALUES ($1, 'free')
		RETURNING id, name, plan, created_at, updated_at, deleted_at
	`, name).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Plan,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return &tenant, nil
}

// CreateStream creates a new stream for a tenant
func CreateStream(db *sql.DB, tenantID, streamName, description string, retentionDays int) (*models.Stream, error) {
	// Validate inputs
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	if streamName == "" {
		return nil, fmt.Errorf("stream name cannot be empty")
	}
	if len(streamName) > 100 {
		return nil, fmt.Errorf("stream name cannot exceed 100 characters")
	}
	if retentionDays <= 0 {
		return nil, fmt.Errorf("retention days must be positive")
	}
	if retentionDays > 365 {
		return nil, fmt.Errorf("retention days cannot exceed 365")
	}

	// Generate Redpanda topic name: stream-<tenant-id>-<stream-name>
	// Sanitize for topic name (lowercase, replace spaces/special chars with hyphens)
	topicName := fmt.Sprintf("stream-%s-%s",
		strings.ToLower(strings.ReplaceAll(tenantID, "-", "")),
		strings.ToLower(strings.ReplaceAll(streamName, " ", "-")))

	// Remove any remaining invalid characters
	topicName = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, topicName)

	var stream models.Stream

	err := db.QueryRow(`
		INSERT INTO streams (tenant_id, name, description, retention_days, topic, status)
		VALUES ($1, $2, $3, $4, $5, 'active')
		RETURNING id, tenant_id, name, description, status, retention_days, topic, created_at, updated_at, deleted_at
	`, tenantID, streamName, description, retentionDays, topicName).Scan(
		&stream.ID,
		&stream.TenantID,
		&stream.Name,
		&stream.Description,
		&stream.Status,
		&stream.RetentionDays,
		&stream.Topic,
		&stream.CreatedAt,
		&stream.UpdatedAt,
		&stream.DeletedAt,
	)

	if err != nil {
		// Check if it's a unique constraint violation
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "23505" { // unique_violation
				return nil, fmt.Errorf("stream '%s' already exists for this tenant", streamName)
			}
		}
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return &stream, nil
}

// GetStream retrieves a stream by ID or name
func GetStream(db *sql.DB, tenantID, streamIdentifier string) (*models.Stream, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	if streamIdentifier == "" {
		return nil, fmt.Errorf("stream identifier cannot be empty")
	}

	var stream models.Stream

	// Try by ID first (UUID format)
	var query string
	var args []interface{}

	if len(streamIdentifier) == 36 && strings.Contains(streamIdentifier, "-") {
		// Looks like a UUID
		query = `
			SELECT id, tenant_id, name, description, status, retention_days, topic, created_at, updated_at, deleted_at
			FROM streams
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{streamIdentifier, tenantID}
	} else {
		// Try by name
		query = `
			SELECT id, tenant_id, name, description, status, retention_days, topic, created_at, updated_at, deleted_at
			FROM streams
			WHERE name = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{streamIdentifier, tenantID}
	}

	err := db.QueryRow(query, args...).Scan(
		&stream.ID,
		&stream.TenantID,
		&stream.Name,
		&stream.Description,
		&stream.Status,
		&stream.RetentionDays,
		&stream.Topic,
		&stream.CreatedAt,
		&stream.UpdatedAt,
		&stream.DeletedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stream '%s' not found", streamIdentifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get stream: %w", err)
	}

	return &stream, nil
}

// ListStreams lists all streams for a tenant
func ListStreams(db *sql.DB, tenantID string) ([]*models.Stream, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}

	rows, err := db.Query(`
		SELECT id, tenant_id, name, description, status, retention_days, topic, created_at, updated_at, deleted_at
		FROM streams
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query streams: %w", err)
	}
	defer rows.Close()

	var streams []*models.Stream
	for rows.Next() {
		var stream models.Stream
		err := rows.Scan(
			&stream.ID,
			&stream.TenantID,
			&stream.Name,
			&stream.Description,
			&stream.Status,
			&stream.RetentionDays,
			&stream.Topic,
			&stream.CreatedAt,
			&stream.UpdatedAt,
			&stream.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan stream: %w", err)
		}
		streams = append(streams, &stream)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating streams: %w", err)
	}

	return streams, nil
}

// DeleteStream soft-deletes a stream by setting deleted_at
func DeleteStream(db *sql.DB, tenantID, streamIdentifier string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant ID cannot be empty")
	}
	if streamIdentifier == "" {
		return fmt.Errorf("stream identifier cannot be empty")
	}

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

	if err != nil {
		return fmt.Errorf("failed to delete stream: %w", err)
	}

	return nil
}
