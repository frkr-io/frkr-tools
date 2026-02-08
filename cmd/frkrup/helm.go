package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// installHelmChart installs or upgrades the frkr Helm chart
func (km *KubernetesManager) installHelmChart(updatedImages map[string]bool) error {
	helmPath, err := findInfraRepoPath("helm")
	if err != nil {
		return fmt.Errorf("failed to find helm chart: %w", err)
	}

	fmt.Println("\n📥 Installing/Upgrading frkr Helm chart...")

	// 0. Pre-flight Check: Stale Data Detection
	// If we are provisioning infrastructure with DEFAULT credentials, we must ensure
	// there are no existing PVCs (disks) from previous installs.
	// Postgres/Redpanda will NOT update their password/config if they see an existing data directory.
	provisionPostgres := km.config.DBHost == "frkr-db"
	if provisionPostgres && km.config.DBPassword == "" {
		// Check for Postgres PVC
		if err := exec.Command("kubectl", "get", "pvc", "data-frkr-db-0").Run(); err == nil {
			return fmt.Errorf(`
⛔ STALE DATA DETECTED!

You are attempting a fresh install (with generated password), but an existing Postgres disk was found.
The database will ignore the new password and try to use the old one, causing authentication failures.

👉 ACTION REQUIRED: Delete the stale data
   kubectl delete pvc data-frkr-db-0

(Or providing the existing password in frkrup.yaml)`)
		}
	}

	provisionRedpanda := km.config.BrokerHost == "frkr-redpanda"
	if provisionRedpanda {
		// Check for Redpanda PVC
		if err := exec.Command("kubectl", "get", "pvc", "datadir-frkr-redpanda-0").Run(); err == nil {
             // We warn for Redpanda as it's less critical for auth, but good practice
			fmt.Println("⚠️  Warning: Existing Redpanda data found (datadir-frkr-redpanda-0).")
		}
	}

	// Generate timestamped values file
	timestamp := time.Now().Format("20060102-150405")
	valuesFilename := fmt.Sprintf("frkr-values-%s.yaml", timestamp)
	valuesPath := filepath.Join(os.TempDir(), valuesFilename)

	if err := km.generateValuesFile(valuesPath); err != nil {
		return fmt.Errorf("failed to generate values file: %w", err)
	}
	fmt.Printf("📄 Generated Helm values: %s\n", valuesPath)

	// Dependency Build (Ensure charts/ is up to date)
	// 0. Ensure repos exist (Prerequisite for dependency build)
	if err := km.ensureHelmRepos(); err != nil {
		fmt.Printf("⚠️  Failed to add helm repos: %v (trying to proceed)\n", err)
	}

	fmt.Println("🧩 Checking/Building chart dependencies...")
	depCmd := exec.Command("helm", "dependency", "build")
	depCmd.Dir = helmPath
	if output, err := depCmd.CombinedOutput(); err != nil {
		fmt.Printf("⚠️  Dependency build warning (non-fatal?): %s\n", string(output))
		// We log but don't fail, in case it's just a network hiccup and charts exist
	} else {
		fmt.Println("✅ Dependencies built")
	}

	// Construct Helm args with generated values file
	args := []string{"upgrade", "--install", "frkr", ".", "-f", "values-full.yaml", "-f", valuesPath}

	cmd := exec.Command("helm", args...)
	cmd.Dir = helmPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade failed: %w", err)
	}

	// Restart deployments if images changed
	if len(updatedImages) > 0 {
		toRestart := []string{}
		for dep, changed := range updatedImages {
			if changed {
				toRestart = append(toRestart, dep)
			}
		}
		if len(toRestart) > 0 {
			fmt.Printf("🔄 Restarting %d deployments...\n", len(toRestart))
			for _, dep := range toRestart {
				exec.Command("kubectl", "rollout", "restart", "deployment", dep).Run()
			}
		}
	}

	return nil
}

// generateValuesFile creates a YAML values file from frkrup config
func (km *KubernetesManager) generateValuesFile(path string) error {
	// Dynamic Infrastructure Defaults (Gap Remediation)
	provisionPostgres := km.config.DBHost == "frkr-db"
	provisionRedpanda := km.config.BrokerHost == "frkr-redpanda"

	// 1. Ensure DB Password if provisioning
	// 1. Ensure DB Password if provisioning
	if provisionPostgres && km.config.DBPassword == "" {
		newPass, err := generateSecurePassword(16)
		if err != nil {
			return fmt.Errorf("failed to generate secure password: %w", err)
		}
		km.config.DBPassword = newPass
		fmt.Printf("\n🔐 Generated secure DB password: %s\n", km.config.DBPassword)
		fmt.Println("   (You should save this, or set db_password in frkrup.yaml for persistence)")
	}

	// Build values structure
	registry := km.config.ImageRegistry
	if registry != "" && !strings.HasSuffix(registry, "/") {
		registry += "/"
	}

	valsGlobal := map[string]interface{}{
		"imageRegistry": registry,
	}
	if km.config.Rebuild {
		// If we just rebuilt/pushed, force pull to get the new bits
		valsGlobal["imagePullPolicy"] = "Always"
	}

	values := map[string]interface{}{
		"global": valsGlobal,
		"platform": map[string]interface{}{
			"k8sGatewayAPI": map[string]interface{}{
				"install": false, // CRDs installed by frkrup before Helm runs
			},
		},
		"infrastructure": map[string]interface{}{
			"db": map[string]interface{}{
				"user":     km.config.DBUser,
				"password": km.config.DBPassword,
				"name":     km.config.DBName,
			},
			"mockOIDC": map[string]interface{}{
				"enabled": km.config.TestOIDC,
			},
		},
		"dataPlane": map[string]interface{}{
			"db": map[string]interface{}{
				"user":     km.config.DBUser,
				"password": km.config.DBPassword,
				"database": km.config.DBName,
				"port":     km.config.DBPort,
			},
		},
	}


	
	if provisionPostgres {
		values["infrastructure"].(map[string]interface{})["postgres"] = map[string]interface{}{
			"provision": true,
			"storage": map[string]interface{}{
				"size": "10Gi",
			},
		}
		// Ensure Config matches internal expectation
		values["infrastructure"].(map[string]interface{})["db"].(map[string]interface{})["host"] = "frkr-db"
	}
	
	
	if provisionRedpanda {
		values["infrastructure"].(map[string]interface{})["redpanda"] = map[string]interface{}{
			"provision": true,
		}
	}

	// Add auth config (Unified Logic)
	authConfig := map[string]interface{}{
		"type": "oidc",
		"oidc": map[string]interface{}{},
	}

	if km.config.OidcIssuer != "" {
		// Real OIDC Provider
		authConfig["oidc"] = map[string]interface{}{
			"issuer":   km.config.OidcIssuer,
			"clientId": km.config.OidcClientId,
		}
		if km.config.OidcClientSecret != "" {
			authConfig["oidc"].(map[string]interface{})["clientSecret"] = km.config.OidcClientSecret
		}

		// Configure Gateways to verify audience
		if km.config.OidcClientId != "" {
			gatewayConfig := map[string]interface{}{
				"config": map[string]interface{}{
					"auth": map[string]interface{}{
						"audience": km.config.OidcClientId,
					},
				},
			}
			values["frkr-ingest-gateway"] = gatewayConfig
			values["frkr-streaming-gateway"] = gatewayConfig
		}
	} else if km.config.TestOIDC {
		// Mock OIDC Provider
		authConfig["oidc"] = map[string]interface{}{
			"issuer": MockOIDCIssuerURL,
		}
	}

	// Apply global auth if either Mock or Real is configured
	if km.config.OidcIssuer != "" || km.config.TestOIDC {
		if _, ok := values["global"]; !ok {
			values["global"] = map[string]interface{}{}
		}
		values["global"].(map[string]interface{})["auth"] = authConfig
	}

	// Add external access config
	switch km.config.ExternalAccess {
	case "loadbalancer":
		values["ingestGateway"] = map[string]interface{}{
			"service": map[string]interface{}{"type": "LoadBalancer"},
		}
		values["streamingGateway"] = map[string]interface{}{
			"service": map[string]interface{}{"type": "LoadBalancer"},
		}
	case "ingress":
		ingress := map[string]interface{}{}
		if km.config.IngressHost != "" {
			ingress["host"] = km.config.IngressHost
		}
		
		// TLS Configuration
		if km.config.IngressTLSSecret != "" || km.config.InstallCertManager {
			tls := map[string]interface{}{
				"enabled": true,
			}
			if km.config.IngressTLSSecret != "" {
				tls["secretName"] = km.config.IngressTLSSecret
			}
			ingress["tls"] = tls
		}
		
		// Cert Manager Annotations
		if km.config.InstallCertManager {
			ingress["annotations"] = map[string]string{
				"cert-manager.io/cluster-issuer": km.config.CertIssuerName,
			}
		}
		
		values["ingress"] = ingress
	}

	// Add vendor provider
	if km.config.Provider != "" {
		values["global"] = map[string]interface{}{
			"provider": km.config.Provider,
		}
	}

	if km.config.InstallCertManager {
		values["platform"].(map[string]interface{})["certManager"].(map[string]interface{})["install"] = true
	}

	// Marshal to YAML
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal values: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write values file: %w", err)
	}

	return nil
}

func (km *KubernetesManager) ensureHelmRepos() error {
	// Add Jetstack for cert-manager
	// We do this aggressively to ensure 'helm dep build' succeeds
	fmt.Println("   Adding Helm repos...")
	cmd := exec.Command("helm", "repo", "add", "jetstack", "https://charts.jetstack.io")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("jetstack repo add failed: %s: %w", string(output), err)
	}
	
	return nil
}

func generateSecurePassword(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
