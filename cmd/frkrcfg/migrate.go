package main

import (
	"fmt"

	"github.com/frkr-io/frkr-common/migrate"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long:  `Run database migrations from the migrations directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dbURL == "" {
			return fmt.Errorf("--db-url is required")
		}

		migrationsPath, _ := cmd.Flags().GetString("migrations-path")

		if migrationsPath == "" {
			migrationsPath = "./migrations"
		}

		if err := migrate.RunMigrations(dbURL, migrationsPath); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		fmt.Println("✅ Migrations completed successfully")
		return nil
	},
}

func init() {
	migrateCmd.Flags().String("migrations-path", "./migrations", "Path to migrations directory")
}
