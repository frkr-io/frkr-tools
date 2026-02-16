package main

import (
	"testing"
)

// --- Validation tests ---

func TestValidateConfig_MutualExclusion_SharedAndPerService(t *testing.T) {
	config := &FrkrupConfig{
		K8s:                  true,
		DBHost:               "frkr-db",
		BrokerHost:           "frkr-redpanda",
		IngressHost:          "frkr.example.com",
		IngestIngressHost:    "ingest.frkr.example.com",
		StreamingIngressHost: "stream.frkr.example.com",
	}
	err := validateConfig(config)
	if err == nil {
		t.Fatal("expected error when both ingress_host and per-service hosts are set")
	}
	if err.Error() != "ingress_host and per-service hosts (ingest_ingress_host, streaming_ingress_host) are mutually exclusive" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_MutualExclusion_SharedAndOnePerService(t *testing.T) {
	config := &FrkrupConfig{
		K8s:               true,
		DBHost:             "frkr-db",
		BrokerHost:         "frkr-redpanda",
		IngressHost:        "frkr.example.com",
		IngestIngressHost:  "ingest.frkr.example.com",
	}
	err := validateConfig(config)
	if err == nil {
		t.Fatal("expected error when ingress_host and one per-service host are set")
	}
}

func TestValidateConfig_MutualExclusion_OnlyOneServiceHost(t *testing.T) {
	config := &FrkrupConfig{
		K8s:               true,
		DBHost:             "frkr-db",
		BrokerHost:         "frkr-redpanda",
		IngestIngressHost:  "ingest.frkr.example.com",
	}
	err := validateConfig(config)
	if err == nil {
		t.Fatal("expected error when only one per-service host is set")
	}
	if err.Error() != "both ingest_ingress_host and streaming_ingress_host must be specified together" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_ValidSharedHost(t *testing.T) {
	config := &FrkrupConfig{
		K8s:         true,
		DBHost:      "frkr-db",
		BrokerHost:  "frkr-redpanda",
		IngressHost: "frkr.example.com",
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_ValidSubdomainHosts(t *testing.T) {
	config := &FrkrupConfig{
		K8s:                  true,
		DBHost:               "frkr-db",
		BrokerHost:           "frkr-redpanda",
		IngestIngressHost:    "ingest.frkr.example.com",
		StreamingIngressHost: "stream.frkr.example.com",
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_ValidNoHosts(t *testing.T) {
	config := &FrkrupConfig{
		K8s:        true,
		DBHost:     "frkr-db",
		BrokerHost: "frkr-redpanda",
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- URL builder tests ---

func TestBuildIngestGatewayURL_Local(t *testing.T) {
	config := &FrkrupConfig{
		IngestPort: 8082,
	}
	got := config.BuildIngestGatewayURL()
	want := "http://localhost:8082/health"
	if got != want {
		t.Errorf("BuildIngestGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildIngestGatewayURL_Ingress(t *testing.T) {
	config := &FrkrupConfig{
		K8s:            true,
		ExternalAccess: "ingress",
		IngressHost:    "frkr.example.com",
	}
	got := config.BuildIngestGatewayURL()
	want := "http://frkr.example.com/health"
	if got != want {
		t.Errorf("BuildIngestGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildIngestGatewayURL_IngressTLS(t *testing.T) {
	config := &FrkrupConfig{
		K8s:              true,
		ExternalAccess:   "ingress",
		IngressHost:      "frkr.example.com",
		IngressTLSSecret: "frkr-tls",
	}
	got := config.BuildIngestGatewayURL()
	want := "https://frkr.example.com/health"
	if got != want {
		t.Errorf("BuildIngestGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildIngestGatewayURL_Subdomain(t *testing.T) {
	config := &FrkrupConfig{
		K8s:               true,
		ExternalAccess:    "ingress",
		IngestIngressHost: "ingest.frkr.example.com",
	}
	got := config.BuildIngestGatewayURL()
	want := "http://ingest.frkr.example.com/health"
	if got != want {
		t.Errorf("BuildIngestGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildIngestGatewayURL_SubdomainTLS(t *testing.T) {
	config := &FrkrupConfig{
		K8s:               true,
		ExternalAccess:    "ingress",
		IngestIngressHost: "ingest.frkr.example.com",
		IngressTLSSecret:  "frkr-tls",
	}
	got := config.BuildIngestGatewayURL()
	want := "https://ingest.frkr.example.com/health"
	if got != want {
		t.Errorf("BuildIngestGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildStreamingGatewayURL_Local(t *testing.T) {
	config := &FrkrupConfig{
		StreamingPort: 8081,
	}
	got := config.BuildStreamingGatewayURL()
	want := "http://localhost:9081/health"
	if got != want {
		t.Errorf("BuildStreamingGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildStreamingGatewayURL_Ingress(t *testing.T) {
	config := &FrkrupConfig{
		K8s:            true,
		ExternalAccess: "ingress",
		IngressHost:    "frkr.example.com",
	}
	got := config.BuildStreamingGatewayURL()
	want := "http://frkr.example.com/health"
	if got != want {
		t.Errorf("BuildStreamingGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildStreamingGatewayURL_Subdomain(t *testing.T) {
	// In subdomain mode, streaming health check uses the ingest host
	// because the streaming gateway only serves gRPC (no HTTP /health).
	config := &FrkrupConfig{
		K8s:                  true,
		ExternalAccess:       "ingress",
		IngestIngressHost:    "ingest.frkr.example.com",
		StreamingIngressHost: "stream.frkr.example.com",
	}
	got := config.BuildStreamingGatewayURL()
	want := "http://ingest.frkr.example.com/health"
	if got != want {
		t.Errorf("BuildStreamingGatewayURL() = %q, want %q", got, want)
	}
}

func TestBuildStreamingGatewayURL_SubdomainTLS(t *testing.T) {
	config := &FrkrupConfig{
		K8s:                  true,
		ExternalAccess:       "ingress",
		IngestIngressHost:    "ingest.frkr.example.com",
		StreamingIngressHost: "stream.frkr.example.com",
		IngressTLSSecret:     "frkr-tls",
	}
	got := config.BuildStreamingGatewayURL()
	want := "https://ingest.frkr.example.com/health"
	if got != want {
		t.Errorf("BuildStreamingGatewayURL() = %q, want %q", got, want)
	}
}
