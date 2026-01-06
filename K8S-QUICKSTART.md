# Kubernetes Quick Start Guide

Get frkr running on Kubernetes in 4 steps.

## Prerequisites

- Docker
- kind (Kubernetes in Docker)
- kubectl
- helm 3.13+
- Go 1.21+
- Node.js 18+

**Install kind:**
```bash
# macOS/Linux
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

# Or via package manager
# macOS: brew install kind
# Linux: See https://kind.sigs.k8s.io/docs/user/quick-start/#installation
```

**Install helm:**
```bash
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

---

## Step 1: Create Kubernetes Cluster

```bash
# Create a kind cluster
kind create cluster --name frkr

# Verify cluster is running
kubectl cluster-info --context kind-frkr
```

**Note:** `frkrup` will use this existing cluster. It will not create one for you.

---

## Step 2: Clone Repositories

```bash
# Clone frkr-tools with submodules (includes infrastructure, Helm charts, and operators)
git clone --recurse-submodules https://github.com/frkr-io/frkr-tools.git

# Clone the example API
git clone https://github.com/frkr-io/frkr-example-api.git
```

> [!IMPORTANT]
> If you already cloned `frkr-tools` without submodules, run:
> ```bash
> cd frkr-tools
> git submodule update --init --recursive
> ```

---

## Step 3: Deploy frkr to Kubernetes

**Recommended: Use `frkrup` (simplified, auto-detects everything)**

```bash
cd frkr-tools

# Build frkrup
make build

# Run frkrup (it will auto-detect your cluster and ask minimal questions)
./bin/frkrup
```

**Alternative: Use Makefile (for automation/CI)**

```bash
# Single command to build all images, setup Kind cluster, and deploy
make kind-up deploy
```

**What happens:**
1. **Auto-Detection**: `frkrup` automatically detects if you have a Kubernetes cluster available
2. **Cluster Detection**: If you have a kind cluster, it auto-detects and defaults to port forwarding
3. **Build & Load**: Builds Docker images for gateways and operator, and loads them into the cluster
4. **Automated Deployment**: Installs the Helm chart (Infrastructure, Operator, Gateways)
5. **Automated Migrations**: Waits for the Helm migration job to complete automatically
6. **Operator Reconciliation**: The `frkr-operator` automatically creates necessary tenants and Kafka topics
7. **Port Forwarding**: For kind clusters, automatically sets up port forwarding for local access

**Simplified prompts:**
- **First question**: "Deploy to Kubernetes? (yes/no) [yes/no]"
  - Defaults to "yes" if kubectl is available and connected to a cluster
  - Defaults to "no" if no cluster detected
  - Just press Enter to accept the default
- **Port forwarding**: "Use port forwarding for local access? (yes/no) [yes/no]"
  - Defaults to "yes" for kind clusters (auto-detected)
  - Defaults to "no" for managed clusters
  - Just press Enter to accept the default
- **External access** (only asked if port forwarding is "no"):
  - Choose LoadBalancer, Ingress, or None
  - For Ingress: prompted for hostname and optional TLS secret

**Verification:**
- Ingest Gateway: `http://localhost:8082/health` (or port you configured)
- Streaming Gateway: `http://localhost:8081/health` (or port you configured)

**For Production Deployments:**
When deploying to a managed Kubernetes cluster (e.g., EKS, GKE, AKS):
1. Answer "no" to port forwarding (default for managed clusters)
2. `frkrup` will ask how to expose services:
   - **LoadBalancer**: Patches services to `type: LoadBalancer`, which triggers your cloud provider to automatically provision a load balancer (ELB/ALB on AWS, Cloud Load Balancer on GCP, Azure Load Balancer on Azure). This costs money but is the easiest option.
     - `frkrup` waits up to 5 minutes for external IPs to be assigned
     - You'll get direct external IPs: `http://<external-ip>:8080` (ingest) and `http://<external-ip>:8081` (streaming)
   - **Ingress**: Creates an Ingress resource using your existing Ingress Controller (supports TLS)
     - You will be prompted for a hostname (e.g., `frkr.example.com`)
     - You can optionally provide a TLS secret name for secure HTTPS access
     - Gateways accessible at: `http://<hostname>/ingest` and `http://<hostname>/streaming`
   - **None**: ClusterIP only (internal access, no external exposure)

---

## Step 4: Start Example API

**In a new terminal:**

```bash
cd frkr-example-api

# Install dependencies
npm install

# Start the API (uses defaults: http://localhost:8082, stream: my-api)
npm start
```

The API runs on:
- **Port 3000**: Direct API calls (mirrored to frkr)
- **Port 3001**: Forwarded requests from frkr CLI

---

## Test It

**In another terminal:**

```bash
# Send a test request (will be mirrored to frkr)
curl http://localhost:3000/api/users

# Send a POST request
curl -X POST http://localhost:3000/api/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice"}'
```

Watch the `frkrup` terminal - you should see the requests being ingested!

**Verify in Kubernetes:**
```bash
# Check pods are running
kubectl get pods

# View gateway logs
kubectl logs -l app.kubernetes.io/component=ingest-gateway --tail=50
kubectl logs -l app.kubernetes.io/component=streaming-gateway --tail=50
```

---

## Stream with CLI

To forward mirrored requests back to your API:

```bash
git clone https://github.com/frkr-io/frkr-cli.git
cd frkr-cli
make build

./bin/frkr stream my-api \
  --gateway-url=http://localhost:8081 \
  --username=testuser \
  --password=testpass \
  --forward-url=http://localhost:3001
```

Watch the `frkr-example-api` terminal - you'll see mirrored requests labeled as **FORWARDED FROM FRKR**.

---

## What's Next?

- **Route-based routing**: See [Node SDK README](https://github.com/frkr-io/frkr-sdk-node/README.md#route-based-stream-routing) for sending different routes to different streams
- **Local Docker setup**: See [Quick Start Guide](QUICKSTART.md) for Docker Compose setup
- **Full documentation**: See [README](README.md) for advanced usage

---

## Troubleshooting

**Cluster not found?**
```bash
# Verify cluster exists
kind get clusters

# If missing, recreate
kind create cluster --name frkr
```

**Wrong kubectl context?**
```bash
# Check current context
kubectl config current-context

# List all contexts
kubectl config get-contexts

# Switch to the correct context (e.g., kind-frkr)
kubectl config use-context kind-frkr

# Verify connection
kubectl cluster-info
```

**Note:** `frkrup` can be run from any terminal - it uses the kubectl context from `~/.kube/config`, not terminal-specific environment variables.

**Auto-detection:**
- `frkrup` automatically detects if you have a Kubernetes cluster available
- For kind clusters, it auto-detects from the kubectl context name (`kind-*`)
- For managed clusters, it detects from the context and defaults to external access options

**Port forwarding:**
- For kind clusters: Port forwarding is set up automatically (default: yes)
- For managed clusters: Port forwarding is skipped by default, external access is configured instead
- If port forwarding fails:
  - Ensure `frkrup` is still running (it maintains port forwarding)
  - Check if ports are already in use: `lsof -i :8082` or `lsof -i :8081`
  - Restart `frkrup` to re-establish port forwarding

**Pods not ready?**
```bash
# Check pod status
kubectl get pods

# View pod events
kubectl describe pod <pod-name>

# View logs
kubectl logs <pod-name>
```

**Can't connect to gateways?**
- For kind clusters: Verify port forwarding is active (should be automatic)
- For managed clusters: Check LoadBalancer IPs or Ingress addresses
- Verify port forwarding: `kubectl port-forward list`
- Check service endpoints: `kubectl get endpoints`
- Verify services exist: `kubectl get svc`

**Migration job issues?**
- Migrations run automatically via Helm hooks (no manual step needed)
- Check migration job status: `kubectl get jobs -l app.kubernetes.io/name=frkr`
- View migration logs: `kubectl logs -l job-name=frkr-migrations`
- The job is automatically cleaned up after successful completion

**Need to reset everything?**
```bash
# Delete the Helm release
helm uninstall frkr

# Delete the cluster
kind delete cluster --name frkr

# Recreate and restart from Step 1
```

---

## Cleanup

To completely remove frkr from your cluster:

```bash
# Stop frkrup (Ctrl+C) to stop port forwarding

# Uninstall Helm release
helm uninstall frkr

# Optional: Delete the cluster
kind delete cluster --name frkr
```

