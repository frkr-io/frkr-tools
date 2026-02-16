# Production Deployment Guide: DigitalOcean

This guide details how to deploy `frkr` to a production environment on **DigitalOcean Kubernetes (DOKS)**.

## Prerequisites

- **DigitalOcean Account**: With credentials to create clusters and Load Balancers.
- **Domain Name**: For SSL/TLS (e.g., `api.example.com`).
- **Tools**:
  - `doctl` (DigitalOcean CLI)
  - `kubectl`
  - `frkrup` (from `frkr-tools`)

## Step 1: Create Kubernetes Cluster

Use `doctl` to create a cost-effective 2-node cluster.

```bash
# Create cluster (SF03 region, 2 nodes of s-2vcpu-4gb is a good baseline)
doctl k8s cluster create frkr-prod \
  --region sf03 \
  --node-pool "name=frkr-pool;size=s-2vcpu-4gb;count=2"

# Configure kubectl
doctl k8s cluster kubeconfig save frkr-prod
```

## Step 2: Configure & Deploy (Automated)

We will use `frkrup` in **non-interactive mode** to automate the entire setup, including `cert-manager`, TLS, and LoadBalancer.

1.  **Create `frkr-prod.yaml`**:

    ```yaml
    k8s: true
    k8s_cluster_name: frkr-prod
    
    # Production Settings
    skip_port_forward: true
    external_access: ingress
    
    # Automated TLS Infrastructure
    install_cert_manager: true
    cert_manager_email: "your-email@example.com"
    # ingress_host: "frkr.example.com"  # Set after getting the external IP
    # ingress_tls_secret: frkr-tls
    ```

2.  **Run Deployment**:

    ```bash
    ./bin/frkrup --config frkr-prod.yaml
    ```

    **What `frkrup` does for you:**
    - **Infrastructure**: Installs Gateway API CRDs, Envoy Gateway, and cert-manager.
    - **TLS Setup**: Creates a `ClusterIssuer` and `Certificate` resource via the Helm chart.
    - **App Deployment**: Deploys all `frkr` components.
    - **Networking**: Provisions a DigitalOcean LoadBalancer via the Envoy Gateway.

3.  **Wait for IP**:
    `frkrup` will wait until DigitalOcean assigns an external IP address and print it out.

## Step 3: DNS & TLS Configuration

To enable HTTPS, follow the **[TLS Setup Guide](TLS-SETUP.md)**.

It explains how to use:
*   **Magic DNS** (`sslip.io`) for instant SSL.
*   **Custom Domains** for production.

**Briefly**:
1.  Get the Envoy Gateway external IP (`kubectl get svc -n envoy-gateway-system`).
2.  Add `ingress_host`, `ingress_tls_secret`, and TLS settings to your `frkrup.yaml`.
3.  Run `frkrup` again.

## Step 5: Verification

```bash
curl https://api.yourdomain.com/health
```
