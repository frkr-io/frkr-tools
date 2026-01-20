package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/frkr-io/frkr-common/util"
	"github.com/frkr-io/frkr-tools/pkg/db"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
	Long:  `Create, list, and manage users.`,
}

var userCreateCmd = &cobra.Command{
	Use:   "create [username]",
	Short: "Create a new user",
	Long:  `Create a new user with auto-generated or user-provided password.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := args[0]

		// Validate username
		if err := util.ValidateUsername(username); err != nil {
			return err
		}

		conn, err := getDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		// Get tenant
		tenant, err := db.CreateOrGetTenant(conn, tenantName)
		if err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		// Get password from flag or generate one
		password, _ := cmd.Flags().GetString("password")
		if password == "" {
			// Generate password using shared utility
			var err error
			password, err = util.GeneratePassword()
			if err != nil {
				return fmt.Errorf("failed to generate password: %w", err)
			}
		}

		// Create user in database
		user, err := db.CreateUser(conn, tenant.ID, username, password)
		if err != nil {
			// If users table doesn't exist, provide helpful error message
			if strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("users table does not exist - please run migrations first: %w", err)
			}
			return fmt.Errorf("failed to create user: %w", err)
		}


		if outputFormat == "json" {
			out := map[string]string{
				"id":          user.ID,
				"username":    username,
				"password":    password,
				"tenant_id":   tenant.ID,
				"tenant_name": tenant.Name,
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "✅ User created successfully!\n\n")
		fmt.Fprintf(cmd.OutOrStdout(), "User ID:       %s\n", user.ID)
		fmt.Fprintf(cmd.OutOrStdout(), "Username:      %s\n", username)
		fmt.Fprintf(cmd.OutOrStdout(), "Password:      %s\n", password)
		fmt.Fprintf(cmd.OutOrStdout(), "Tenant:        %s (%s)\n\n", tenant.Name, tenant.ID)
		fmt.Fprintf(cmd.OutOrStdout(), "⚠️  Save this password - it won't be shown again!\n")

		return nil
	},
}

func init() {
	userCreateCmd.Flags().String("password", "", "User password (if not provided, a random password will be generated)")
	userCmd.AddCommand(userCreateCmd)
}
