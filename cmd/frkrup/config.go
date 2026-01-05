package main

import (
	"fmt"
	"os/exec"
)

// Config holds the configuration for frkrup setup
type Config struct {
	K8s            bool
	K8sClusterName string
	DBHost         string
	DBPort         string
	DBUser         string
	DBPassword     string
	DBName         string
	BrokerHost     string
	BrokerPort     string
	BrokerUser     string
	BrokerPassword string
	IngestPort     int
	StreamingPort  int
	MigrationsPath string
	StreamName     string
	CreateStream   bool
	StartedDocker  bool      // Track if we started Docker Compose
	IngestCmd      *exec.Cmd // Track ingest gateway process
	StreamingCmd   *exec.Cmd // Track streaming gateway process
}

// BuildDBURL constructs a PostgreSQL connection URL from the config
func (c *Config) BuildDBURL() string {
	var parts []string
	if c.DBUser != "" {
		if c.DBPassword != "" {
			parts = append(parts, fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName))
		} else {
			parts = append(parts, fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
				c.DBUser, c.DBHost, c.DBPort, c.DBName))
		}
	} else {
		parts = append(parts, fmt.Sprintf("postgres://%s:%s/%s?sslmode=disable",
			c.DBHost, c.DBPort, c.DBName))
	}
	return parts[0]
}

// BuildBrokerURL constructs a broker connection URL from the config
func (c *Config) BuildBrokerURL() string {
	return fmt.Sprintf("%s:%s", c.BrokerHost, c.BrokerPort)
}
