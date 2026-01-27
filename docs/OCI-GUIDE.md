# Production Deployment Guide: Oracle Cloud (Free Tier)

This guide details how to deploy `frkr` to a production-ready environment on **Oracle Cloud Infrastructure (OCI)**, specifically leveraging their **Always Free Tier**.

## Architecture for Free Tier

- **Cluster**: OKE (Oracle Kubernetes Engine) on **Ampere A1 (ARM64)** instances (Always Free allows up to 4 OCPUs and 24GB RAM).
- **Load Balancer**: OCI Flexible Load Balancer (10Mbps min/max fits Always Free).
- **TLS/DNS**: Automated via `cert-manager` and `sslip.io` (Magic DNS).

> **Note**: `frkr` images are multi-arch and support ARM64 out of the box.

## Prerequisites

- **OCI Account**: Valid account with Free Tier eligibility.
- **kubectl**: Installed.
- **Helm**: Installed (v3.x).
- **OpenTofu**: Installed (v1.6+). *Terraform v1.0+ also works.*
- **frkrup**: Installed (v0.2.0+ required for OCI support).

## Step 1: Install & Configure OCI CLI

The OCI Command Line Interface (CLI) is required for **OpenTofu/Terraform authentication**. It generates the API keys and configuration file (`~/.oci/config`) that the deployment tools rely on.

1.  **Install OCI CLI**: Follow the [official installation guide](https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm).
2.  **Run Setup**:
    ```bash
    oci setup config
    ```
3.  **Follow Prompts**: This will guide you through creating an API key, uploading it to your Oracle Console, and saving your Tenancy and User OCIDs.

> **Important**: Ensure your `~/.oci/config` is generated and valid before proceeding.

## Step 2: Create Kubernetes Cluster (ARM64)

You can create the OKE cluster manually via the OCI Console/CLI, or use our Terraform/OpenTofu preset (recommended).

### Option A: OpenTofu / Terraform (Recommended)

The `frkr-infra-terraform` submodule provides a ready-to-use OCI Free Tier preset.

> **Note**: Examples use `tofu`, but `terraform` commands are drop-in compatible.

1.  **Generate SSH Keys**:
    You'll need an SSH key pair to access the cluster nodes for debugging if needed.
    ```bash
    ssh-keygen -t ed25519 -f ~/.ssh/oci-frkr -C "frkr-oci-cluster"
    ```

2.  **Initialize Infrastructure**:
    ```bash
    cd frkr-infra-terraform/presets/oci-free-tier
    cp terraform.tfvars.example terraform.tfvars
    
    # Edit terraform.tfvars with your OCI credentials (Tenancy ID, User ID, etc.)
    # and the path to your public key (~/.ssh/oci-frkr.pub)
    
    tofu init
    tofu plan -out=tfplan
    ```

3.  **Verify Free Tier Compliance**:
    Run the verification script to ensure your plan fits within the Always Free limits.
    ```bash
    ../../scripts/verify-oci-free-tier.sh
    ```

4.  **Apply**:
    ```bash
    tofu apply tfplan
    ```

5.  **Configure Access**:
    ```bash
    export KUBECONFIG=$(tofu output -raw kubeconfig_path)
    kubectl cluster-info
    ```

### Option B: Manual (OCI Console)

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

## Step 3: Create Deployment Config

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

## Step 4: Run Deployment

Deploy `frkr` using the configuration:

```bash
./bin/frkrup --config frkr-oci.yaml
```

**What happens next:**
1.  **Helm Values**: `frkrup` generates a timestamped values file (e.g., `/tmp/frkr-values-20260124-103000.yaml`) for reproducibility.
2.  **K8s Gateway API CRDs**: Installed via Helm pre-install hook (not `frkrup` directly).
3.  **Helm Chart**: Installed with `global.provider=oci`, automatically configuring the Flexible Load Balancer.
4.  **Infrastructure**: Cert-Manager and specialized Gateways are deployed.
5.  **Load Balancer**: OCI provisions a Flexible Load Balancer. This may take 2-4 minutes. `frkrup` will wait.

> **Tip**: You can inspect the generated values file to see exactly what was passed to Helm.

## Step 5: Configure TLS (Magic DNS)

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

## Step 6: Alternative: Custom Domain with Cloudflare

If you own a domain (e.g., `example.com`), point an `A` record to the LoadBalancer IP.

1.  **DNS**: Create `A api.example.com` -> `123.45.67.89`.
2.  **Update Config**:
    ```yaml
    ingress_host: api.example.com
    ingress_tls_secret: frkr-tls
    ```
3.  **Apply**: Run `frkrup` again.

## Troubleshooting

### Error: Out of host capacity (500 InternalError)

If you see an error like:
> `Error returned by LaunchInstance operation ... Out of host capacity`

This is a common issue with OCI's **Always Free** tier, specifically for the high-demand **Ampere A1 (ARM64)** instances. It means the region you selected is currently full for free tier allocations.

**Solutions:**
1.  **Retry**: Capacity fluctuates constantly. Wait 10-20 minutes and run `tofu apply` again. Scripts that loop and retry (auto-clickers) are common in the community for this reason.
2.  **Upgrade Account**: Upgrading to a "Pay-As-You-Go" account (requires a credit card) often gives you higher priority for these instances, even if you stay within the "Always Free" usage limits (first 3000 OCPU-hours/month are free).
3.  **Change Region**: If your tenancy allows, try deploying to a different OCI region (e.g., `us-ashburn-1`, `us-phoenix-1`, `eu-frankfurt-1`). *Note: You may need to subscribe to the region in the OCI Console first.*

## Post-Deployment Verification

After deployment, run the verification script to confirm everything is working:

```bash
./scripts/test-helm-deployment.sh
```

This checks:
- K8s Gateway API CRDs are installed
- Core frkr components are deployed
- cert-manager is running (if enabled)
