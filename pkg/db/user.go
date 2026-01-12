package db

import (
	"database/sql"

	commondb "github.com/frkr-io/frkr-common/db"
	"github.com/frkr-io/frkr-common/models"
)

// TenantUser aliases the common model
type TenantUser = models.TenantUser

// CreateUser creates a new user for a tenant
func CreateUser(db *sql.DB, tenantID, username, password string) (*models.TenantUser, error) {
	return commondb.CreateUser(db, tenantID, username, password)
}

// GetUser retrieves a user by username or ID
func GetUser(db *sql.DB, tenantID, userIdentifier string) (*models.TenantUser, error) {
	return commondb.GetUser(db, tenantID, userIdentifier)
}

// ListUsers lists all users for a tenant
func ListUsers(db *sql.DB, tenantID string) ([]*models.TenantUser, error) {
	return commondb.ListUsers(db, tenantID)
}

// VerifyPassword verifies a password against a user's password hash
func VerifyPassword(user *models.TenantUser, password string) error {
	return commondb.VerifyPassword(user, password)
}

