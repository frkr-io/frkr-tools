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
