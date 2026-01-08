package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// ClientCredential represents a client credential in the system
type ClientCredential struct {
	ID           string
	TenantID     string
	StreamID     sql.NullString
	ClientID     string
	ClientSecret string
	CreatedAt    sql.NullTime
	UpdatedAt    sql.NullTime
	DeletedAt    *sql.NullTime
}

// CreateClient creates a new client credential for a tenant, optionally scoped to a stream
func CreateClient(db *sql.DB, tenantID, clientID, clientSecret string, streamID *string) (*ClientCredential, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	if clientID == "" {
		return nil, fmt.Errorf("client ID cannot be empty")
	}
	if len(clientID) > 255 {
		return nil, fmt.Errorf("client ID cannot exceed 255 characters")
	}
	for _, r := range clientID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return nil, fmt.Errorf("client ID can only contain alphanumeric characters, dashes, and underscores")
		}
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client secret cannot be empty")
	}
	if len(clientSecret) < 8 {
		return nil, fmt.Errorf("client secret must be at least 8 characters")
	}

	var client ClientCredential
	var streamIDVal sql.NullString
	if streamID != nil && *streamID != "" {
		streamIDVal = sql.NullString{String: *streamID, Valid: true}
	}

	err := db.QueryRow(`
		INSERT INTO clients (tenant_id, stream_id, client_id, client_secret)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, stream_id, client_id, client_secret, created_at, updated_at, deleted_at
	`, tenantID, streamIDVal, clientID, clientSecret).Scan(
		&client.ID,
		&client.TenantID,
		&client.StreamID,
		&client.ClientID,
		&client.ClientSecret,
		&client.CreatedAt,
		&client.UpdatedAt,
		&client.DeletedAt,
	)

	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "23505" {
				return nil, fmt.Errorf("client ID '%s' already exists for this tenant", clientID)
			}
			if pgErr.Code == "42P01" {
				return nil, fmt.Errorf("clients table does not exist - migrations may not have been run")
			}
			if pgErr.Code == "23503" {
				if strings.Contains(pgErr.Message, "tenant_id") {
					return nil, fmt.Errorf("tenant ID '%s' does not exist", tenantID)
				}
				if strings.Contains(pgErr.Message, "stream_id") {
					return nil, fmt.Errorf("stream ID '%s' does not exist", *streamID)
				}
			}
		}
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &client, nil
}

// GetClient retrieves a client by client ID or UUID
func GetClient(db *sql.DB, tenantID, clientIdentifier string) (*ClientCredential, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	if clientIdentifier == "" {
		return nil, fmt.Errorf("client identifier cannot be empty")
	}

	var client ClientCredential
	var query string
	var args []interface{}

	if len(clientIdentifier) == 36 && strings.Contains(clientIdentifier, "-") {
		query = `
			SELECT id, tenant_id, stream_id, client_id, client_secret, created_at, updated_at, deleted_at
			FROM clients
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{clientIdentifier, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, stream_id, client_id, client_secret, created_at, updated_at, deleted_at
			FROM clients
			WHERE client_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{clientIdentifier, tenantID}
	}

	err := db.QueryRow(query, args...).Scan(
		&client.ID,
		&client.TenantID,
		&client.StreamID,
		&client.ClientID,
		&client.ClientSecret,
		&client.CreatedAt,
		&client.UpdatedAt,
		&client.DeletedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("client '%s' not found", clientIdentifier)
	}
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "42P01" {
				return nil, fmt.Errorf("clients table does not exist - migrations may not have been run")
			}
		}
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return &client, nil
}

// ListClients lists all clients for a tenant, optionally filtered by stream
func ListClients(db *sql.DB, tenantID string, streamID *string) ([]*ClientCredential, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}

	var query string
	var args []interface{}

	if streamID != nil && *streamID != "" {
		query = `
			SELECT id, tenant_id, stream_id, client_id, client_secret, created_at, updated_at, deleted_at
			FROM clients
			WHERE tenant_id = $1 AND stream_id = $2 AND deleted_at IS NULL
			ORDER BY created_at DESC
		`
		args = []interface{}{tenantID, *streamID}
	} else {
		query = `
			SELECT id, tenant_id, stream_id, client_id, client_secret, created_at, updated_at, deleted_at
			FROM clients
			WHERE tenant_id = $1 AND deleted_at IS NULL
			ORDER BY created_at DESC
		`
		args = []interface{}{tenantID}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "42P01" {
				return nil, fmt.Errorf("clients table does not exist - migrations may not have been run")
			}
		}
		return nil, fmt.Errorf("failed to query clients: %w", err)
	}
	defer rows.Close()

	var clients []*ClientCredential
	for rows.Next() {
		var client ClientCredential
		err := rows.Scan(
			&client.ID,
			&client.TenantID,
			&client.StreamID,
			&client.ClientID,
			&client.ClientSecret,
			&client.CreatedAt,
			&client.UpdatedAt,
			&client.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan client: %w", err)
		}
		clients = append(clients, &client)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clients: %w", err)
	}

	return clients, nil
}
