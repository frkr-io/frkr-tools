# Production Deployment Guide: Generic Kubernetes

This guide details how to deploy `frkr` to **any standard Kubernetes cluster** (EKS, GKE, DigitalOcean, On-Prem via K3s/RKE2, etc.).

## Prerequisites

1.  **Kubernetes Cluster**: Version 1.25+ recommended.
2.  **Storage**: A Default `StorageClass` must be configured (for persistent volumes).
3.  **LoadBalancer**: Support for `LoadBalancer` services (or use Ingress).
4.  **Tools**: `kubectl` and `frkrup` CLI installed.

## 1. Verify Connectivity

Ensure your `kubectl` context points to the target cluster:

```bash
kubectl config current-context
# Should return your target cluster name
```

## 2. Configuration (`frkrup.yaml`)

Create a configuration file. Note that `k8s_cluster_name` is used for **Safety Validation** only (it must match your active context name).

```yaml
k8s: true
k8s_cluster_name: "my-production-cluster-name" # MUST match 'kubectl config current-context'

# Networking: "ingress" for Envoy Gateway (recommended), or omit for ClusterIP + port-forward
external_access: "ingress"

# Database
db_host: "frkr-db"
db_name: "frkr"
# Leave db_password EMPTY to auto-generate a secure random password

# OIDC (Optional)
# oidc_issuer: "https://accounts.google.com"
# oidc_client_id: "..."
```

> For TLS/HTTPS, see the [TLS Setup Guide](TLS-SETUP.md).

## 3. Deployment

Run the universal installer:

```bash
bin/frkrup --config frkrup.yaml
```

The installer will:
1.  Validate your context match.
2.  Check for stale data collisions (safety check).
3.  Install Gateway API CRDs.
4.  Install Envoy Gateway controller (if `external_access: ingress`).
5.  Install cert-manager (if `install_cert_manager: true`).
6.  Deploy the frkr Helm chart.

## Troubleshooting

### "Context Mismatch" Error
`frkrup` prevents accidental deployments by verifying the target. If you get this error:
```text
context mismatch! Active Context: kind-dev ...
```
Simply switch your kubectl context:
```bash
kubectl config use-context <correct-cluster-name>
```

### "Stale Data Detected" Error
If you redeploy to a fresh namespace that has leftover disks (PVCs) from a previous install, `frkrup` will halt to prevent password mismatches.
**Fix**: `kubectl delete pvc data-frkr-db-0`
