# Production Deployment Guide: Oracle Cloud (Free Tier)

This guide details how to deploy `frkr` to a production-ready environment on **Oracle Cloud Infrastructure (OCI)**, specifically leveraging their **Always Free Tier**.

## Architecture for Free Tier

- **Cluster**: OKE (Oracle Kubernetes Engine) on **Ampere A1 (ARM64)** instances (Always Free allows up to 4 OCPUs and 24GB RAM).
- **Load Balancer**: OCI Flexible Load Balancer (10Mbps min/max fits Always Free).
- **TLS/DNS**: Automated via `cert-manager` and `sslip.io` (Magic DNS).

> **Note**: `frkr` images are multi-arch and support ARM64 out of the box.

## Prerequisites

- **OCI Account**: Valid account with Free Tier eligibility.
- **OCI CLI**: Installed and configured (`oci setup config`).
- **kubectl**: Installed.
- **frkrup**: Installed (v0.2.0+ required for OCI support).

## Step 1: Create Kubernetes Cluster (ARM64)

We will use the OCI Console or CLI to create a generic OKE cluster.

1.  **Network**: Create a VCN with public subnets (easiest for access) or use the "Quick Create" wizard in OKE.
2.  **Cluster**: Create an OKE cluster.
3.  **Node Pool**:
    - **Image**: Oracle Linux Cloud Developer (or generic Oracle Linux).
    - **Shape**: `VM.Standard.A1.Flex` (ARM64).
    - **Size**: 2 OCPUs, 12GB RAM (Fits comfortably in free tier).
    - **Nodes**: 1 or 2.

4.  **Configure kubectl**:
    ```bash
    oci ce cluster create-kubeconfig --cluster-id <YOUR_CLUSTER_ID> --file $HOME/.kube/config --region <YOUR_REGION> --token-version 2.0.0  --kube-endpoint PUBLIC_ENDPOINT
    ```

## Step 2: Create Deployment Config

Create a `frkr-oci.yaml` configuration file. This configures `frkrup` to deploy a LoadBalancer compatible with OCI's Free Tier constraints.

```yaml
k8s: true
k8s_cluster_name: frkr-oci     # Optional, for logging

# Production Settings
skip_port_forward: true
external_access: loadbalancer

# OCI Free Tier Configuration
# Setting the provider tells the Helm chart to apply OCI-specific presets 
# (e.g. Flexible Load Balancer annotations)
provider: oci

# TLS Configuration (Automated)
install_cert_manager: true
cert_manager_email: "your-email@example.com"

# Gateway Configuration (Standard Ports)
ingest_port: 8080
streaming_port: 8081
```

## Step 3: Run Deployment

Deploy `frkr` using the configuration:

```bash
./bin/frkrup --config frkr-oci.yaml
```

**What happens next:**
1.  **Helm Chart**: Installed with `global.provider=oci`, automatically configuring the Flexible Load Balancer.
2.  **Infrastructure**: Cert-Manager and specialized Gateways are deployed.
3.  **Load Balancer**: OCI provisions a Flexible Load Balancer. This may take 2-4 minutes. `frkrup` will wait.

## Step 4: Configure TLS (Magic DNS)

Once `frkrup` finishes, it will print the LoadBalancer IP (e.g., `123.45.67.89`).

To enable TLS without buying a domain, we use `sslip.io`:

1.  **Update `frkr-oci.yaml`**:
    ```yaml
    external_access: ingress
    ingress_host: 123-45-67-89.sslip.io   # Replace dashes with your IP
    ingress_tls_secret: frkr-tls
    
    # Keep previous settings
    provider: oci
    install_cert_manager: true
    cert_manager_email: "your-email@example.com"
    ```

2.  **Apply Update**:
    ```bash
    ./bin/frkrup --config frkr-oci.yaml
    ```

3.  **Verification**:
    Wait for Let's Encrypt challenge to complete (approx. 1-2 mins).
    
    ```bash
    curl https://123-45-67-89.sslip.io/health
    ```
    You should receive a valid TLS response! 🔒

## Step 5: Alternative: Custom Domain with Cloudflare

If you own a domain (e.g., `example.com`), point an `A` record to the LoadBalancer IP.

1.  **DNS**: Create `A api.example.com` -> `123.45.67.89`.
2.  **Update Config**:
    ```yaml
    ingress_host: api.example.com
    ingress_tls_secret: frkr-tls
    ```
3.  **Apply**: Run `frkrup` again.
