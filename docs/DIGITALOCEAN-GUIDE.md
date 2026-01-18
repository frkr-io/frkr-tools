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
    external_access: loadbalancer
    
    # Automated TLS Infrastructure
    install_cert_manager: true
    cert_manager_email: "your-email@example.com"
    
    # Advanced: Customize Issuer/Ingress (Defaults shown)
    # cert_issuer_name: "letsencrypt-prod"
    # ingress_class_name: "envoy"
    
    # Gateway Configuration
    ingest_port: 8080
    streaming_port: 8081
    ```

2.  **Run Deployment**:

    ```bash
    ./bin/frkrup --config frkr-prod.yaml
    ```

    **What `frkrup` does for you:**
    - **Infrastructure**: Installs Gateway API CRDs, Envoy Gateway, and **Cert-Manager** (v1.14.4).
    - **TLS Setup**: Configures a Let's Encrypt Production **ClusterIssuer** using your email.
    - **App Deployment**: Deploys all `frkr` components.
    - **Networking**: Provisions a DigitalOcean LoadBalancer.

3.  **Wait for IP**:
    `frkrup` will wait until DigitalOcean assigns an external IP address and print it out.

## Step 3: DNS & TLS Configuration

You have two options: **Standard Custom Domain** or **Magic DNS**.

### Option A: Magic DNS (Zero-Config)

If you have no domain, use `sslip.io` with your new IP.

1.  **Get IP**: From `frkrup` output (e.g., `157.245.1.1`).
2.  **Update Config**:
    Modify `frkr-prod.yaml` to secure the gateway using the magic domain:
    ```yaml
    external_access: ingress
    ingress_host: 157-245-1-1.sslip.io
    ingress_tls_secret: frkr-tls
    # Keep infrastructure flags (idempotent)
    install_cert_manager: true
    cert_manager_email: "your-email@example.com"
    ```
3.  **Apply**:
    ```bash
    ./bin/frkrup --config frkr-prod.yaml
    ```
    *Result*: `https://157-245-1-1.sslip.io/health` is fully secure.

### Option B: Custom Domain

1.  **DNS**: Create an `A` record pointing `api.yourdomain.com` to the LoadBalancer IP.
2.  **Update Config**:
    ```yaml
    external_access: ingress
    ingress_host: api.yourdomain.com
    ingress_tls_secret: frkr-tls
    ...
    ```
3.  **Apply**: Run `frkrup` again.

## Step 5: Verification

```bash
curl https://api.yourdomain.com/health
```
