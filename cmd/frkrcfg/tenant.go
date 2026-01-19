package main

import (
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

		fmt.Printf("✅ Tenant '%s' ready\n", tenant.Name)
		fmt.Printf("ID: %s\n", tenant.ID)
	},
}

func init() {
	tenantCmd.AddCommand(tenantCreateCmd)
}
