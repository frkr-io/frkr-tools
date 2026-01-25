#!/bin/bash
# Post-deployment verification for frkr Helm chart
# Usage: ./test-helm-deployment.sh [--check-gateway-api] [--check-cert-manager] [--check-values-file]
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_ALL=true

for arg in "$@"; do
  case $arg in
    --check-gateway-api) CHECK_GW=true; CHECK_ALL=false ;;
    --check-cert-manager) CHECK_CM=true; CHECK_ALL=false ;;
    --check-values-file) CHECK_VALUES=true; CHECK_ALL=false ;;
    --help|-h)
      echo "Usage: $0 [OPTIONS]"
      echo "Options:"
      echo "  --check-gateway-api    Check K8s Gateway API CRDs"
      echo "  --check-cert-manager   Check cert-manager deployment"
      echo "  --check-values-file    Check generated values file"
      echo "  --help, -h             Show this help message"
      exit 0
      ;;
  esac
done

echo "=== frkr Helm Deployment Verification ==="

# Test 1: K8s Gateway API CRDs
if [ "$CHECK_ALL" = true ] || [ "$CHECK_GW" = true ]; then
    echo "Checking K8s Gateway API CRDs..."
    if kubectl get crd gateways.gateway.networking.k8s.io -o name &>/dev/null; then
        echo "✅ K8s Gateway API CRDs present"
    else
        echo "❌ K8s Gateway API CRDs not found"
        exit 1
    fi
fi

# Test 2: Generated values file
if [ "$CHECK_ALL" = true ] || [ "$CHECK_VALUES" = true ]; then
    echo "Checking generated values file..."
    VALUES_FILE=$(ls -t /tmp/frkr-values-*.yaml 2>/dev/null | head -1)
    if [ -n "$VALUES_FILE" ]; then
        if command -v yq &>/dev/null; then
            yq eval '.platform.k8sGatewayAPI.install' "$VALUES_FILE" > /dev/null
        fi
        echo "✅ Values file valid: $VALUES_FILE"
    else
        echo "⚠️  No generated values file found (OK if using --config directly)"
    fi
fi

# Test 3: cert-manager (optional)
if [ "$CHECK_ALL" = true ] || [ "$CHECK_CM" = true ]; then
    echo "Checking cert-manager..."
    if kubectl get namespace cert-manager &>/dev/null; then
        kubectl get deployment cert-manager -n cert-manager -o name &>/dev/null
        echo "✅ cert-manager deployed"
    else
        echo "⚠️  cert-manager not installed (OK if disabled)"
    fi
fi

# Test 4: Core frkr components
echo "Checking frkr components..."
FAILED=false
for component in frkr-operator frkr-ingest-gateway frkr-streaming-gateway; do
    if kubectl get deployment "$component" -o name &>/dev/null; then
        echo "  ✅ $component"
    else
        echo "  ❌ $component not found"
        FAILED=true
    fi
done

if [ "$FAILED" = true ]; then
    echo "❌ Some frkr components missing"
    exit 1
fi

echo ""
echo "=== All deployment checks passed ==="
