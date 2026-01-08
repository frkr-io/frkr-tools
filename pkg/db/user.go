package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/frkr-io/frkr-common/util"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// TenantUser represents a user in the system
type TenantUser struct {
	ID           string
	TenantID     string
	Username     string
	PasswordHash string
	CreatedAt    sql.NullTime
	UpdatedAt    sql.NullTime
	DeletedAt    *sql.NullTime
}

// CreateUser creates a new user for a tenant
func CreateUser(db *sql.DB, tenantID, username, password string) (*TenantUser, error) {
	// Validate inputs
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	// Use shared username validation
	if err := util.ValidateUsername(username); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var user TenantUser

	// Check if users table exists (graceful handling if migration not run yet)
	err = db.QueryRow(`
		INSERT INTO users (tenant_id, username, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, username, password_hash, created_at, updated_at, deleted_at
	`, tenantID, username, string(passwordHash)).Scan(
		&user.ID,
		&user.TenantID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	)

	if err != nil {
		// Check if it's a unique constraint violation
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "23505" { // unique_violation
				return nil, fmt.Errorf("username '%s' already exists for this tenant", username)
			}
			if pgErr.Code == "42P01" { // undefined_table
				return nil, fmt.Errorf("users table does not exist - migrations may not have been run")
			}
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

// GetUser retrieves a user by username or ID
func GetUser(db *sql.DB, tenantID, userIdentifier string) (*TenantUser, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}
	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier cannot be empty")
	}

	var user TenantUser
	var query string
	var args []interface{}

	// Try by ID first (UUID format)
	if len(userIdentifier) == 36 && strings.Contains(userIdentifier, "-") {
		// Looks like a UUID
		query = `
			SELECT id, tenant_id, username, password_hash, created_at, updated_at, deleted_at
			FROM users
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{userIdentifier, tenantID}
	} else {
		// Try by username
		query = `
			SELECT id, tenant_id, username, password_hash, created_at, updated_at, deleted_at
			FROM users
			WHERE username = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`
		args = []interface{}{userIdentifier, tenantID}
	}

	err := db.QueryRow(query, args...).Scan(
		&user.ID,
		&user.TenantID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user '%s' not found", userIdentifier)
	}
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "42P01" { // undefined_table
				return nil, fmt.Errorf("users table does not exist - migrations may not have been run")
			}
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// ListUsers lists all users for a tenant
func ListUsers(db *sql.DB, tenantID string) ([]*TenantUser, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID cannot be empty")
	}

	rows, err := db.Query(`
		SELECT id, tenant_id, username, password_hash, created_at, updated_at, deleted_at
		FROM users
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok {
			if pgErr.Code == "42P01" { // undefined_table
				return nil, fmt.Errorf("users table does not exist - migrations may not have been run")
			}
		}
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []*TenantUser
	for rows.Next() {
		var user TenantUser
		err := rows.Scan(
			&user.ID,
			&user.TenantID,
			&user.Username,
			&user.PasswordHash,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}

// VerifyPassword verifies a password against a user's password hash
func VerifyPassword(user *TenantUser, password string) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return fmt.Errorf("invalid password")
	}

	return nil
}
