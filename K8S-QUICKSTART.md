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
# Clone frkr-tools with submodules (includes infrastructure and Helm charts)
git clone --recurse-submodules https://github.com/frkr-io/frkr-tools.git

# Clone the example API
git clone https://github.com/frkr-io/frkr-example-api.git
```

---

## Step 3: Deploy frkr to Kubernetes

```bash
cd frkr-tools

# Build and run frkrup
make build
./bin/frkrup
```

**Note:** You can run `frkrup` from any terminal - it doesn't need to be the same terminal where you created the kind cluster. `frkrup` uses `kubectl` which reads from `~/.kube/config` (global configuration). Just ensure:
- The kind cluster exists: `kind get clusters`
- The correct kubectl context is set: `kubectl config current-context` (should show `kind-frkr` or similar)

**When prompted:**
1. **Deploy to Kubernetes?** → Type `yes`
2. **Use port forwarding for local access?** → Type `yes` (for local development with kind)
3. **Kubernetes cluster name** → Press Enter (auto-detected from `kind-frkr` context) or enter your cluster name

**What happens:**
1. `frkrup` builds Docker images for both gateways
2. Loads images into your kind cluster
3. Upgrades/installs the Helm chart (includes CockroachDB, Redpanda, Operator, and Gateways)
4. If upgrading, restarts gateway deployments to use new images
5. Waits for all pods to be ready
6. Sets up port forwarding (if enabled):
   - Ingest Gateway: `http://localhost:8082`
   - Streaming Gateway: `http://localhost:8081`
7. Runs database migrations
8. Verifies gateways are healthy

**Keep this terminal open** - `frkrup` maintains port forwarding. Press `Ctrl+C` to stop port forwarding and exit (k8s resources will remain provisioned).

**For Production Deployments:** When deploying to a managed Kubernetes cluster (e.g., EKS, GKE, AKS), answer "no" to port forwarding. `frkrup` will show you how to configure LoadBalancer services or Ingress for external access.

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

**Port forwarding fails?**
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
- Verify port forwarding is active: `kubectl port-forward list`
- Check service endpoints: `kubectl get endpoints`
- Verify services exist: `kubectl get svc`

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

