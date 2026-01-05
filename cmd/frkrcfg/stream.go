package main

import (
	"fmt"

	"github.com/frkr-io/frkr-common/util"
	"github.com/frkr-io/frkr-tools/pkg/db"
	"github.com/spf13/cobra"
)

var streamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Manage streams",
	Long:  `Create, list, and manage streams.`,
}

var streamCreateCmd = &cobra.Command{
	Use:   "create [stream-name]",
	Short: "Create a new stream",
	Long:  `Create a new stream for message mirroring.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		streamName := args[0]

		// Use shared stream name validation
		if err := util.ValidateStreamName(streamName); err != nil {
			return err
		}

		description, _ := cmd.Flags().GetString("description")
		retentionDays, _ := cmd.Flags().GetInt("retention-days")

		// Normalize and validate retention days
		normalizedDays, err := util.NormalizeRetentionDays(retentionDays)
		if err != nil {
			return err
		}
		retentionDays = normalizedDays

		conn, err := getDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		// Create or get tenant
		tenant, err := db.CreateOrGetTenant(conn, tenantName)
		if err != nil {
			return fmt.Errorf("failed to create/get tenant: %w", err)
		}

		// Create stream
		stream, err := db.CreateStream(conn, tenant.ID, streamName, description, retentionDays)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}

		// Output stream information
		fmt.Printf("✅ Stream created successfully!\n\n")
		fmt.Printf("Stream ID:     %s\n", stream.ID)
		fmt.Printf("Stream Name:   %s\n", stream.Name)
		fmt.Printf("Tenant:        %s (%s)\n", tenant.Name, tenant.ID)
		fmt.Printf("Topic:         %s\n", stream.Topic)
		fmt.Printf("Retention:     %d days\n", stream.RetentionDays)
		fmt.Printf("Status:        %s\n\n", stream.Status)
		fmt.Printf("Use this stream ID in your SDK:\n")
		fmt.Printf("  streamId: '%s'\n", stream.Name)

		return nil
	},
}

var streamListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all streams",
	Long:  `List all streams for a tenant.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		// List streams
		streams, err := db.ListStreams(conn, tenant.ID)
		if err != nil {
			return fmt.Errorf("failed to list streams: %w", err)
		}

		if len(streams) == 0 {
			fmt.Printf("No streams found for tenant '%s'\n", tenantName)
			return nil
		}

		fmt.Printf("Streams for tenant '%s':\n\n", tenantName)
		fmt.Printf("%-36s %-20s %-15s %-30s\n", "ID", "Name", "Status", "Topic")
		fmt.Printf("%s\n", "------------------------------------------------------------------------------------------------")
		for _, stream := range streams {
			fmt.Printf("%-36s %-20s %-15s %-30s\n",
				stream.ID,
				stream.Name,
				stream.Status,
				stream.Topic)
		}

		return nil
	},
}

var streamGetCmd = &cobra.Command{
	Use:   "get [stream-name-or-id]",
	Short: "Get stream details",
	Long:  `Get details for a specific stream.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		streamIdentifier := args[0]

		if streamIdentifier == "" {
			return fmt.Errorf("stream identifier cannot be empty")
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

		// Get stream
		stream, err := db.GetStream(conn, tenant.ID, streamIdentifier)
		if err != nil {
			return fmt.Errorf("failed to get stream: %w", err)
		}

		fmt.Printf("Stream Details:\n\n")
		fmt.Printf("ID:            %s\n", stream.ID)
		fmt.Printf("Name:          %s\n", stream.Name)
		fmt.Printf("Description:   %s\n", stream.Description)
		fmt.Printf("Status:        %s\n", stream.Status)
		fmt.Printf("Retention:     %d days\n", stream.RetentionDays)
		fmt.Printf("Topic:         %s\n", stream.Topic)
		fmt.Printf("Tenant ID:     %s\n", stream.TenantID)
		fmt.Printf("Created:       %s\n", stream.CreatedAt.Format("2006-01-02 15:04:05"))

		return nil
	},
}

var streamDeleteCmd = &cobra.Command{
	Use:   "delete [stream-name-or-id]",
	Short: "Delete a stream",
	Long:  `Soft-delete a stream by setting its deleted_at timestamp. The stream will no longer appear in listings.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		streamIdentifier := args[0]

		if streamIdentifier == "" {
			return fmt.Errorf("stream identifier cannot be empty")
		}

		force, _ := cmd.Flags().GetBool("force")

		if !force {
			return fmt.Errorf("deletion requires --force flag for safety")
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

		// Get stream first to show what we're deleting
		stream, err := db.GetStream(conn, tenant.ID, streamIdentifier)
		if err != nil {
			return fmt.Errorf("failed to get stream: %w", err)
		}

		// Delete stream
		err = db.DeleteStream(conn, tenant.ID, streamIdentifier)
		if err != nil {
			return fmt.Errorf("failed to delete stream: %w", err)
		}

		fmt.Printf("✅ Stream deleted successfully!\n\n")
		fmt.Printf("Deleted stream:\n")
		fmt.Printf("  Name: %s\n", stream.Name)
		fmt.Printf("  ID:   %s\n", stream.ID)
		fmt.Printf("  Topic: %s\n", stream.Topic)
		fmt.Printf("\nNote: This is a soft delete. The stream data remains in the database.\n")

		return nil
	},
}

func init() {
	streamCreateCmd.Flags().String("description", "", "Stream description")
	streamCreateCmd.Flags().Int("retention-days", 7, "Retention period in days (default: 7)")

	streamDeleteCmd.Flags().Bool("force", false, "Force deletion (required for safety)")

	streamCmd.AddCommand(streamCreateCmd)
	streamCmd.AddCommand(streamListCmd)
	streamCmd.AddCommand(streamGetCmd)
	streamCmd.AddCommand(streamDeleteCmd)
}
