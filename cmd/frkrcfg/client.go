package main

import (
	"fmt"
	"strings"

	"github.com/frkr-io/frkr-common/db"
	"github.com/frkr-io/frkr-common/util"
	"github.com/spf13/cobra"
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Manage client credentials",
	Long:  `Create, list, and manage OAuth client credentials for SDK authentication.`,
}

var clientCreateCmd = &cobra.Command{
	Use:   "create [client-id]",
	Short: "Create a new client credential",
	Long:  `Create a new OAuth client credential with auto-generated or user-provided secret. Optionally scope to a stream.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID := args[0]

		if clientID == "" {
			return fmt.Errorf("client ID cannot be empty")
		}
		if len(clientID) > 255 {
			return fmt.Errorf("client ID cannot exceed 255 characters")
		}
		for _, r := range clientID {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				return fmt.Errorf("client ID can only contain alphanumeric characters, dashes, and underscores")
			}
		}

		conn, err := getDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		tenant, err := db.CreateOrGetTenant(conn, tenantName)
		if err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		streamName, _ := cmd.Flags().GetString("stream")
		var streamID *string
		if streamName != "" {
			stream, err := db.GetStream(conn, tenant.ID, streamName)
			if err != nil {
				return fmt.Errorf("failed to get stream '%s': %w", streamName, err)
			}
			streamID = &stream.ID
		}

		clientSecret, _ := cmd.Flags().GetString("secret")
		if clientSecret == "" {
			var err error
			clientSecret, err = util.GeneratePassword()
			if err != nil {
				return fmt.Errorf("failed to generate client secret: %w", err)
			}
		}

		client, err := db.CreateClient(conn, tenant.ID, clientID, clientSecret, streamID)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("clients table does not exist - please run migrations first: %w", err)
			}
			return fmt.Errorf("failed to create client: %w", err)
		}

		fmt.Printf("✅ Client credential created successfully!\n\n")
		fmt.Printf("Client ID:     %s\n", client.ClientID)
		fmt.Printf("Client Secret: %s\n", clientSecret)
		fmt.Printf("Tenant:        %s (%s)\n", tenant.Name, tenant.ID)
		if client.StreamID.Valid {
			fmt.Printf("Stream:        %s\n", client.StreamID.String)
		} else {
			fmt.Printf("Stream:        (not scoped to any stream)\n")
		}
		fmt.Printf("Client UUID:   %s\n\n", client.ID)
		fmt.Printf("⚠️  Save this client secret - it won't be shown again!\n")
		fmt.Printf("\nUse in your SDK:\n")
		fmt.Printf("  clientId: '%s'\n", client.ClientID)
		fmt.Printf("  clientSecret: '%s'\n", clientSecret)

		return nil
	},
}

var clientListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all client credentials",
	Long:  `List all client credentials for a tenant, optionally filtered by stream.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := getDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		tenant, err := db.CreateOrGetTenant(conn, tenantName)
		if err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		streamName, _ := cmd.Flags().GetString("stream")
		var streamID *string
		if streamName != "" {
			stream, err := db.GetStream(conn, tenant.ID, streamName)
			if err != nil {
				return fmt.Errorf("failed to get stream '%s': %w", streamName, err)
			}
			streamID = &stream.ID
		}

		clients, err := db.ListClients(conn, tenant.ID, streamID)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("clients table does not exist - please run migrations first: %w", err)
			}
			return fmt.Errorf("failed to list clients: %w", err)
		}

		if len(clients) == 0 {
			streamFilter := ""
			if streamName != "" {
				streamFilter = fmt.Sprintf(" for stream '%s'", streamName)
			}
			fmt.Printf("No clients found for tenant '%s'%s\n", tenantName, streamFilter)
			return nil
		}

		streamFilter := ""
		if streamName != "" {
			streamFilter = fmt.Sprintf(" (filtered by stream '%s')", streamName)
		}
		fmt.Printf("Clients for tenant '%s'%s:\n\n", tenantName, streamFilter)
		fmt.Printf("%-36s %-30s %-30s %-20s\n", "UUID", "Client ID", "Stream", "Created")
		fmt.Printf("%s\n", strings.Repeat("-", 120))
		for _, client := range clients {
			streamDisplay := "(not scoped)"
			if client.StreamID.Valid {
				streamDisplay = client.StreamID.String
			}
			createdAt := "N/A"
			if client.CreatedAt.Valid {
				createdAt = client.CreatedAt.Time.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-36s %-30s %-30s %-20s\n",
				client.ID,
				client.ClientID,
				streamDisplay,
				createdAt)
		}

		return nil
	},
}

var clientGetCmd = &cobra.Command{
	Use:   "get [client-id-or-uuid]",
	Short: "Get client credential details",
	Long:  `Get details for a specific client credential. Note: secrets are not displayed for security reasons.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clientIdentifier := args[0]

		if clientIdentifier == "" {
			return fmt.Errorf("client identifier cannot be empty")
		}

		conn, err := getDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		tenant, err := db.CreateOrGetTenant(conn, tenantName)
		if err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		client, err := db.GetClient(conn, tenant.ID, clientIdentifier)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("clients table does not exist - please run migrations first: %w", err)
			}
			return fmt.Errorf("failed to get client: %w", err)
		}

		fmt.Printf("Client Details:\n\n")
		fmt.Printf("UUID:          %s\n", client.ID)
		fmt.Printf("Client ID:     %s\n", client.ClientID)
		fmt.Printf("Tenant:        %s (%s)\n", tenant.Name, tenant.ID)
		if client.StreamID.Valid {
			fmt.Printf("Stream:        %s\n", client.StreamID.String)
		} else {
			fmt.Printf("Stream:        (not scoped to any stream)\n")
		}
		fmt.Printf("Created:       %s\n", client.CreatedAt.Time.Format("2006-01-02 15:04:05"))
		fmt.Printf("\n⚠️  Client secret is not displayed for security reasons.\n")
		fmt.Printf("If you need to retrieve the secret, you'll need to create a new client.\n")

		return nil
	},
}

func init() {
	clientCreateCmd.Flags().String("secret", "", "Client secret (if not provided, a random secret will be generated)")
	clientCreateCmd.Flags().String("stream", "", "Stream name to scope this client to (optional)")

	clientListCmd.Flags().String("stream", "", "Filter clients by stream name (optional)")

	clientCmd.AddCommand(clientCreateCmd)
	clientCmd.AddCommand(clientListCmd)
	clientCmd.AddCommand(clientGetCmd)
}
