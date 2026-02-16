# Production Deployment Guide: DigitalOcean (DOKS)

This guide details how to deploy `frkr` to **DigitalOcean Kubernetes (DOKS)**. This is a low-cost, low-complexity alternative to OCI or AWS.

## Architecture & Cost

*   **Cluster**: Managed DOKS Control Plane (Free).
*   **Nodes**: 2x `s-2vcpu-4gb` ($12/mo each = **$24/mo**).
*   **Load Balancer**: 1x DigitalOcean LB ($12/mo).
*   **Total Expected Cost: ~$36 / month**.

## Prerequisites

1.  **DigitalOcean Account**: [Sign up here](https://cloud.digitalocean.com/).
2.  **API Token**:
    *   Go to **API** -> **Generate New Token**.
    *   Select scopes: **Read** and **Write**.
3.  **OpenTofu (or Terraform)**: Installed locally.

## Setup Instructions

### 1. Configure Terraform

Navigate to the DigitalOcean preset directory:

```bash
cd frkr-infra-terraform/presets/digitalocean-starter
```

Create your variables file:

```bash
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` and paste your **API Token**:

```hcl
do_token = "dop_v1_..."
region   = "sf03"
```

### 2. Provision Infrastructure

Initialize and apply the configuration:

```bash
tofu init
tofu plan -out tfplan
tofu apply tfplan
```

Type `yes` to confirm. This will take ~5-8 minutes to provision the cluster.

### 3. Verify Access

Once complete, a `kubeconfig` file is generated in the current directory.

```bash
export KUBECONFIG=$(pwd)/kubeconfig
kubectl get nodes
```

You should see 2 nodes in `Ready` status.

### 4. Deploy `frkr`

#### frkrup 

You can now use `frkrup` to deploy to this cluster. Create a `frkrup.yaml`:

```yaml
k8s: true
k8s_cluster_name: "frkr-cluster"
external_access: "ingress"  # Envoy Gateway (provisions DO LoadBalancer automatically)

# OIDC Configuration (Optional but Recommended)
oidc_issuer: "https://idp.example.com/"
oidc_client_id: "your-client-id"
oidc_client_secret: "your-client-secret"
```

> For TLS/HTTPS, see the [TLS Setup Guide](TLS-SETUP.md).

Run the deployment:

```bash
frkrup --config frkrup.yaml
```

#### Helm

```bash
# Example using helm directly (ensure you export KUBECONFIG first)
helm install frkr frkr/frkr-stack
```

*Note: The DigitalOcean LoadBalancer is automatically provisioned when the Envoy Gateway creates a Service of type `LoadBalancer`.*

## Clean Up

To stop billing (~$1.20/day), destroy the resources immediately when finished:

```bash
tofu destroy
```

> [!IMPORTANT]
> Verify in the DigitalOcean Dashboard that the **Load Balancer** and **Volumes** were actually destroyed. Sometimes K8s resources can leave orphans if not deleted cleanly.
