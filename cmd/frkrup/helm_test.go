package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateValuesFile_RealOIDC(t *testing.T) {
	// Setup config
	config := &FrkrupConfig{
		OidcIssuer:       "https://test.auth0.com/",
		OidcClientId:     "client-123",
		OidcClientSecret: "secret-456",
	}

	km := &KubernetesManager{config: config}
	
	// Create temp file
	tmpDir := t.TempDir()
	valuesPath := filepath.Join(tmpDir, "values.yaml")

	// Generate
	err := km.generateValuesFile(valuesPath)
	if err != nil {
		t.Fatalf("generateValuesFile failed: %v", err)
	}

	// Read and Validate
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("failed to read values file: %v", err)
	}

	var values map[string]interface{}
	err = yaml.Unmarshal(data, &values)
	if err != nil {
		t.Fatalf("failed to parse yaml: %v", err)
	}

	// Verify global.auth structure
	global, ok := values["global"].(map[string]interface{})
	if !ok {
		t.Fatal("global key missing or invalid")
	}

	auth, ok := global["auth"].(map[string]interface{})
	if !ok {
		t.Fatal("global.auth key missing")
	}

	if auth["type"] != "oidc" {
		t.Errorf("expected auth.type=oidc, got %v", auth["type"])
	}

	oidc, ok := auth["oidc"].(map[string]interface{})
	if !ok {
		t.Fatal("global.auth.oidc key missing")
	}

	if oidc["issuer"] != "https://test.auth0.com/" {
		t.Errorf("expected issuer=https://test.auth0.com/, got %v", oidc["issuer"])
	}
	if oidc["clientId"] != "client-123" {
		t.Errorf("expected clientId=client-123, got %v", oidc["clientId"])
	}
	if oidc["clientSecret"] != "secret-456" {
		t.Errorf("expected clientSecret=secret-456, got %v", oidc["clientSecret"])
	}

	// Verify Gateway Config
	ingest, ok := values["frkr-ingest-gateway"].(map[string]interface{})
	if !ok {
		t.Fatal("frkr-ingest-gateway config missing")
	}
	audience := ingest["config"].(map[string]interface{})["auth"].(map[string]interface{})["audience"]
	if audience != "client-123" {
		t.Errorf("expected gateway audience=client-123, got %v", audience)
	}
}

func TestGenerateValuesFile_IngressMode(t *testing.T) {
	config := &FrkrupConfig{
		ExternalAccess:   "ingress",
		IngressHost:      "frkr.example.com",
		IngressTLSSecret: "frkr-tls",
	}

	km := &KubernetesManager{config: config}
	tmpDir := t.TempDir()
	valuesPath := filepath.Join(tmpDir, "values.yaml")

	if err := km.generateValuesFile(valuesPath); err != nil {
		t.Fatalf("generateValuesFile failed: %v", err)
	}

	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("failed to read values file: %v", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("failed to parse yaml: %v", err)
	}

	// Verify ingress config
	ingress, ok := values["ingress"].(map[string]interface{})
	if !ok {
		t.Fatal("ingress key missing or invalid")
	}

	if ingress["host"] != "frkr.example.com" {
		t.Errorf("expected ingress.host=frkr.example.com, got %v", ingress["host"])
	}

	tls, ok := ingress["tls"].(map[string]interface{})
	if !ok {
		t.Fatal("ingress.tls key missing")
	}
	if tls["enabled"] != true {
		t.Errorf("expected ingress.tls.enabled=true, got %v", tls["enabled"])
	}
	if tls["secretName"] != "frkr-tls" {
		t.Errorf("expected ingress.tls.secretName=frkr-tls, got %v", tls["secretName"])
	}

	// Verify no service type overrides (should stay ClusterIP)
	if _, ok := values["ingestGateway"]; ok {
		t.Error("ingestGateway should not have service type override in ingress mode")
	}
	if _, ok := values["streamingGateway"]; ok {
		t.Error("streamingGateway should not have service type override in ingress mode")
	}

	// Verify per-service hosts are NOT set in shared-host mode
	if _, ok := ingress["ingestHost"]; ok {
		t.Error("ingress.ingestHost should not be set in shared-host mode")
	}
	if _, ok := ingress["streamingHost"]; ok {
		t.Error("ingress.streamingHost should not be set in shared-host mode")
	}
}

func TestGenerateValuesFile_SubdomainMode(t *testing.T) {
	config := &FrkrupConfig{
		ExternalAccess:       "ingress",
		IngestIngressHost:    "ingest.frkr.example.com",
		StreamingIngressHost: "stream.frkr.example.com",
		IngressTLSSecret:     "frkr-tls",
	}

	km := &KubernetesManager{config: config}
	tmpDir := t.TempDir()
	valuesPath := filepath.Join(tmpDir, "values.yaml")

	if err := km.generateValuesFile(valuesPath); err != nil {
		t.Fatalf("generateValuesFile failed: %v", err)
	}

	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("failed to read values file: %v", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("failed to parse yaml: %v", err)
	}

	ingress, ok := values["ingress"].(map[string]interface{})
	if !ok {
		t.Fatal("ingress key missing or invalid")
	}

	if ingress["ingestHost"] != "ingest.frkr.example.com" {
		t.Errorf("expected ingress.ingestHost=ingest.frkr.example.com, got %v", ingress["ingestHost"])
	}
	if ingress["streamingHost"] != "stream.frkr.example.com" {
		t.Errorf("expected ingress.streamingHost=stream.frkr.example.com, got %v", ingress["streamingHost"])
	}

	// Shared host should NOT be set
	if _, ok := ingress["host"]; ok {
		t.Error("ingress.host should not be set in subdomain mode")
	}

	tls, ok := ingress["tls"].(map[string]interface{})
	if !ok {
		t.Fatal("ingress.tls key missing")
	}
	if tls["enabled"] != true {
		t.Errorf("expected ingress.tls.enabled=true, got %v", tls["enabled"])
	}
}
