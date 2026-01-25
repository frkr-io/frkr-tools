package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Generate timestamped values file
	timestamp := time.Now().Format("20060102-150405")
	valuesFilename := fmt.Sprintf("frkr-values-%s.yaml", timestamp)
	valuesPath := filepath.Join(os.TempDir(), valuesFilename)

	if err := km.generateValuesFile(valuesPath); err != nil {
		return fmt.Errorf("failed to generate values file: %w", err)
	}
	fmt.Printf("📄 Generated Helm values: %s\n", valuesPath)

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
	// Build values structure
	values := map[string]interface{}{
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

	// Add auth config for mock OIDC
	if km.config.TestOIDC {
		values["auth"] = map[string]interface{}{
			"oidc": map[string]interface{}{
				"issuerUrl": "http://frkr-mock-oidc.default.svc.cluster.local:8080/default",
			},
		}
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
		ingress := map[string]interface{}{"enabled": true}
		if km.config.IngressHost != "" {
			ingress["host"] = km.config.IngressHost
		}
		values["ingress"] = ingress
	}

	// Add vendor provider
	if km.config.Provider != "" {
		values["global"] = map[string]interface{}{
			"provider": km.config.Provider,
		}
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
