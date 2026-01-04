package db

import (
	"fmt"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCreateUser(t *testing.T) {
	db, _ := setupTestDB(t)

	// Create a tenant first
	tenant, err := CreateOrGetTenant(db, "user-test-tenant")
	require.NoError(t, err)

	// Check if users table exists - if not, skip tests
	var tableExists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)

	if !tableExists {
		t.Skip("users table does not exist - skipping user tests. Run migrations to create users table.")
		return
	}

	t.Run("create user", func(t *testing.T) {
		user, err := CreateUser(db, tenant.ID, "testuser", "password123")
		require.NoError(t, err)
		require.NotEmpty(t, user.ID)
		require.Equal(t, tenant.ID, user.TenantID)
		require.Equal(t, "testuser", user.Username)
		require.NotEmpty(t, user.PasswordHash)
		require.True(t, user.CreatedAt.Valid)
	})

	t.Run("duplicate username fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "duplicate-user", "password123")
		require.NoError(t, err)

		// Try to create again
		_, err = CreateUser(db, tenant.ID, "duplicate-user", "password456")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("same username different tenant succeeds", func(t *testing.T) {
		otherTenant, err := CreateOrGetTenant(db, "other-user-tenant")
		require.NoError(t, err)

		user1, err := CreateUser(db, tenant.ID, "shared-username", "password123")
		require.NoError(t, err)

		user2, err := CreateUser(db, otherTenant.ID, "shared-username", "password456")
		require.NoError(t, err)

		require.NotEqual(t, user1.ID, user2.ID)
		require.Equal(t, user1.Username, user2.Username)
		require.NotEqual(t, user1.TenantID, user2.TenantID)
	})
}

func TestCreateUser_EdgeCases(t *testing.T) {
	db, _ := setupTestDB(t)

	// Check if users table exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)

	if !tableExists {
		t.Skip("users table does not exist - skipping user tests")
		return
	}

	tenant, err := CreateOrGetTenant(db, "edge-case-user-tenant")
	require.NoError(t, err)

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := CreateUser(db, "", "testuser", "password123")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty username fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "", "password123")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty password fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "testuser", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("short password fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "testuser", "short")
		require.Error(t, err)
		require.Contains(t, err.Error(), "at least 8 characters")
	})

	t.Run("very long username fails", func(t *testing.T) {
		longName := strings.Repeat("a", 101)
		_, err := CreateUser(db, tenant.ID, longName, "password123")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed")
	})

	t.Run("username with invalid characters fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "invalid@user", "password123")
		require.Error(t, err)
		require.Contains(t, err.Error(), "alphanumeric")
	})

	t.Run("username with spaces fails", func(t *testing.T) {
		_, err := CreateUser(db, tenant.ID, "user name", "password123")
		require.Error(t, err)
		require.Contains(t, err.Error(), "alphanumeric")
	})

	t.Run("valid username formats", func(t *testing.T) {
		validUsernames := []string{
			"user123",
			"user-name",
			"user_name",
			"User123_Name",
			"a",
			"123",
		}

		for i, username := range validUsernames {
			user, err := CreateUser(db, tenant.ID, fmt.Sprintf("%s-%d", username, i), "password123")
			require.NoError(t, err, "username: %s", username)
			require.NotEmpty(t, user.ID)
		}
	})
}

func TestGetUser(t *testing.T) {
	db, _ := setupTestDB(t)

	// Check if users table exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)

	if !tableExists {
		t.Skip("users table does not exist - skipping user tests")
		return
	}

	tenant, err := CreateOrGetTenant(db, "get-user-tenant")
	require.NoError(t, err)

	user, err := CreateUser(db, tenant.ID, "get-test-user", "password123")
	require.NoError(t, err)

	t.Run("get user by username", func(t *testing.T) {
		found, err := GetUser(db, tenant.ID, "get-test-user")
		require.NoError(t, err)
		require.Equal(t, user.ID, found.ID)
		require.Equal(t, user.Username, found.Username)
	})

	t.Run("get user by ID", func(t *testing.T) {
		found, err := GetUser(db, tenant.ID, user.ID)
		require.NoError(t, err)
		require.Equal(t, user.ID, found.ID)
		require.Equal(t, user.Username, found.Username)
	})

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := GetUser(db, "", "testuser")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty user identifier fails", func(t *testing.T) {
		_, err := GetUser(db, tenant.ID, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("non-existent user fails", func(t *testing.T) {
		_, err := GetUser(db, tenant.ID, "non-existent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("user from different tenant not found", func(t *testing.T) {
		otherTenant, err := CreateOrGetTenant(db, "other-get-tenant")
		require.NoError(t, err)

		// Try to get user with wrong tenant ID
		_, err = GetUser(db, otherTenant.ID, user.ID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}

func TestListUsers(t *testing.T) {
	db, _ := setupTestDB(t)

	// Check if users table exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)

	if !tableExists {
		t.Skip("users table does not exist - skipping user tests")
		return
	}

	tenant, err := CreateOrGetTenant(db, "list-users-tenant")
	require.NoError(t, err)

	// Create multiple users
	user1, err := CreateUser(db, tenant.ID, "list-user-1", "password123")
	require.NoError(t, err)

	user2, err := CreateUser(db, tenant.ID, "list-user-2", "password456")
	require.NoError(t, err)

	t.Run("list all users", func(t *testing.T) {
		users, err := ListUsers(db, tenant.ID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(users), 2)

		// Should contain our users
		userIDs := make(map[string]bool)
		for _, u := range users {
			userIDs[u.ID] = true
		}
		require.True(t, userIDs[user1.ID])
		require.True(t, userIDs[user2.ID])
	})

	t.Run("list users for tenant with no users", func(t *testing.T) {
		emptyTenant, err := CreateOrGetTenant(db, "empty-users-tenant")
		require.NoError(t, err)

		users, err := ListUsers(db, emptyTenant.ID)
		require.NoError(t, err)
		require.Len(t, users, 0)
	})

	t.Run("empty tenant ID fails", func(t *testing.T) {
		_, err := ListUsers(db, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestVerifyPassword(t *testing.T) {
	db, _ := setupTestDB(t)

	// Check if users table exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)

	if !tableExists {
		t.Skip("users table does not exist - skipping user tests")
		return
	}

	tenant, err := CreateOrGetTenant(db, "verify-password-tenant")
	require.NoError(t, err)

	password := "correct-password-123"
	user, err := CreateUser(db, tenant.ID, "verify-user", password)
	require.NoError(t, err)

	t.Run("correct password succeeds", func(t *testing.T) {
		err := VerifyPassword(user, password)
		require.NoError(t, err)
	})

	t.Run("incorrect password fails", func(t *testing.T) {
		err := VerifyPassword(user, "wrong-password")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid password")
	})

	t.Run("empty password fails", func(t *testing.T) {
		err := VerifyPassword(user, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("nil user fails", func(t *testing.T) {
		err := VerifyPassword(nil, password)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be nil")
	})
}
