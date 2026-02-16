# Production Deployment Guide: Azure (AKS)

This guide details how to deploy `frkr` to **Azure Kubernetes Service (AKS)**. This preset is designed for **Production** usage, utilizing standard managed services while maintaining a reasonable cost profile.

## Architecture & Cost

*   **Cluster**: Managed AKS (Standard Tier - Free).
*   **Nodes**: 2x `Standard_B2s` (Burstable, 2 vCPU, 4 GB RAM).
    *   *Note*: uses `B2s` (Burstable) for cost efficiency. For sustained high-CPU workloads, consider upgrading to `Standard_D2as_v5` ($130/node).
    *   **Cost**: ~$36/mo each = **$72/mo**.
*   **Storage (OS)**: Optimized 64GB OS Disks (~$5/mo/node) = **$10/mo**.
*   **Storage (Data)**: ~64GB Persistent Volume for DB/Kafka = **~$5/mo** (Standard SSD).
*   **Networking**: Standard Load Balancer + Public IP (~$5/mo).
*   **Registry**: Azure Container Registry (Basic SKU) = **~$5/mo**.
*   **Total Expected Cost: ~$100 / month**.

> [!NOTE]
> **Cost Efficiency**: This configuration is optimized for cost-effective production usage (~$100/mo).
> *   **L7 Ingress (Envoy)**: Runs as high-availability pods on the nodes.
> *   **Security**: The **Azure Standard Load Balancer** provides managed DDoS protection at the network layer (L4). Envoy handles L7 traffic/TLS.
> *   **OIDC (Entra ID)**: Free (Free Tier supports OIDC apps).


## Prerequisites

1.  **Azure CLI**: [Install `az`](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli).
2.  **Login**: Run `az login` to authenticate your shell.
3.  **OpenTofu (or Terraform)**: Installed locally.

## Setup Instructions

### 1. Configure Terraform

Navigate to the Azure preset directory:

```bash
cd frkr-infra-terraform/presets/azure-production
```

(Optional) Create variables file if you want to change the region:

```bash
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` to include your Subscription ID (found via `az account show`):

```hcl
subscription_id = "your-subscription-id"
# edit location = "westus2" if desired
```

### 2. Provision Infrastructure

Initialize and apply. Terraform will use your `az login` credentials automatically.

```bash
tofu init
tofu plan -out tfplan
tofu apply tfplan
```

Type `yes` to confirm. This will take ~5-10 minutes.

**Important Outputs:**
Note the `registry_login_server` and `registry_name` displayed at the end. You will need these.

### 3. Verify Access

Once complete, a `kubeconfig` file is generated in the current directory.

```bash
export KUBECONFIG=$(pwd)/kubeconfig
kubectl get nodes

# Login to your new Container Registry
# Replace 'frkrcrx...' with the registry_name from Terraform output
az acr login --name frkrcrx...
```

### 4. Deploy `frkr`

Create a `frkrup.yaml` to deploy the stack:

```yaml
k8s: true
k8s_cluster_name: "frkr-aks"

# Registry (Required for AKS) - From Terraform output
image_registry: "frkrcrx....azurecr.io"

# Ingress with TLS (Envoy Gateway + cert-manager)
external_access: "ingress"
# ingress_host: "frkr.example.com"    # Set after getting the external IP (see TLS-SETUP.md)
# ingress_tls_secret: frkr-tls
# install_cert_manager: true
# cert_manager_email: "you@example.com"

# OIDC Configuration (Azure AD / Entra ID Example)
# 1. Create App Registration in Entra ID (Azure AD)
# 2. Add Redirect URI: None needed for backend (unless using CLI login flow)
# 3. Use values below:
oidc_issuer: "https://login.microsoftonline.com/YOUR_TENANT_ID/v2.0"
oidc_client_id: "YOUR_APP_CLIENT_ID"
oidc_client_secret: "YOUR_CLIENT_SECRET"
```

> For TLS/HTTPS configuration (including sslip.io for quick testing), see the [TLS Setup Guide](TLS-SETUP.md).

Run the deployment:

```bash
# frkrup will automatically Build & Push images to your ACR before deploying
bin/frkrup --config frkrup.yaml
```

(Optional) If you only want to push images without deploying:
`bin/frkrup push --config frkrup.yaml`

## Clean Up

To avoid burning credits when not in use:

```bash
tofu destroy
```
