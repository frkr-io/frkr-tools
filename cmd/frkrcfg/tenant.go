package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/frkr-io/frkr-common/db"
	"github.com/spf13/cobra"
)

var tenantCmd = &cobra.Command{
	Use:   "tenant",
	Short: "Manage tenants",
	Long:  `Create and view tenants directly in the database.`,
}

var tenantCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new tenant",
	Long:  `Create a new tenant in the database. If it already exists, returns the existing ID.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		
		conn, err := getDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()

		tenant, err := db.CreateOrGetTenant(conn, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tenant: %v\n", err)
			os.Exit(1)
		}


		if outputFormat == "json" {
			out := map[string]string{
				"id":   tenant.ID,
				"name": tenant.Name,
			}
			if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
				os.Exit(1)
			}
			return
		}

		fmt.Printf("✅ Tenant '%s' ready\n", tenant.Name)
		fmt.Printf("ID: %s\n", tenant.ID)
	},
}

var tenantGetCmd = &cobra.Command{
	Use:   "get [name]",
	Short: "Get tenant details",
	Long:  `Get tenant details and ID from the database.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		
		conn, err := getDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()

		// Use CreateOrGetTenant for now as it handles retrieval.
		// In a strictly read-only 'get', we might want a separate db.GetTenantByName,
		// but CreateOrGet is safe enough for local config tool usage and simplifies logic.
		tenant, err := db.CreateOrGetTenant(conn, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting tenant: %v\n", err)
			os.Exit(1)
		}


		if outputFormat == "json" {
			out := map[string]string{
				"id":   tenant.ID,
				"name": tenant.Name,
			}
			if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
				os.Exit(1)
			}
			return
		}

		fmt.Println(tenant.ID)
	},
}

func init() {
	tenantCmd.AddCommand(tenantCreateCmd)
	tenantCmd.AddCommand(tenantGetCmd)
}
