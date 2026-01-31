# Production Deployment Guide: Azure (AKS)

This guide details how to deploy `frkr` to **Azure Kubernetes Service (AKS)**. This preset is designed for **Robust Production** usage, utilizing dedicated CPU instances that fit within a monthly flexible credit (e.g., $150/mo).

## Architecture & Cost

*   **Cluster**: Managed AKS (Standard Tier - Free).
*   **Nodes**: 2x `Standard_B2s` (Burstable, 2 vCPU, 4 GB RAM).
    *   *Note*: We use `B2s` because dedicated `D2` nodes ($130/node) would exceed your budget.
    *   **Cost**: ~$36/mo each = **$72/mo**.
*   **Storage (OS)**: Optimized 64GB OS Disks (~$5/mo/node) = **$10/mo**.
*   **Storage (Data)**: ~64GB Persistent Volume for DB/Kafka = **~$5/mo** (Standard SSD).
*   **Networking**: Standard Load Balancer + Public IP (~$5/mo).
*   **Total Expected Cost: ~$95 / month**.

> [!TIP]
> **Safety First**: This setup leaves you a **$55/mo buffer** (35% of budget).
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
# edit location = "eastus" if desired
```

### 2. Provision Infrastructure

Initialize and apply. Terraform will use your `az login` credentials automatically.

```bash
tofu init
tofu plan -out tfplan
tofu apply tfplan
```

Type `yes` to confirm. This will take ~5-10 minutes.

### 3. Verify Access

Once complete, a `kubeconfig` file is generated in the current directory.

```bash
export KUBECONFIG=$(pwd)/kubeconfig
kubectl get nodes
```

### 4. Deploy `frkr`

Create a `frkrup.yaml` to deploy the stack:

```yaml
k8s: true
k8s_cluster_name: "frkr-aks"
external_access: "loadbalancer" # Uses Azure LB

# OIDC Configuration (Azure AD / Entra ID Example)
# 1. Create App Registration in Entra ID (Azure AD)
# 2. Add Redirect URI: None needed for backend (unless using CLI login flow)
# 3. Use values below:
oidc_issuer: "https://login.microsoftonline.com/YOUR_TENANT_ID/v2.0"
oidc_client_id: "YOUR_APP_CLIENT_ID"
oidc_client_secret: "YOUR_CLIENT_SECRET"
```

Run the deployment:

```bash
frkrup --config frkrup.yaml
```

## Clean Up

To avoid burning credits when not in use:

```bash
tofu destroy
```
