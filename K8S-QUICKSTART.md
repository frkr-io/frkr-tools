# Kubernetes Quick Start Guide

Get frkr running on Kubernetes in 4 steps.

## Prerequisites

- Docker
- A Kubernetes cluster (Kind, minikube, k3d, or a managed cluster)
- kubectl
- helm 3.13+
- Go 1.21+
- Node.js 18+

---

## Step 1: Create Kubernetes Cluster

Use whatever tool you prefer. For example, with Kind:

```bash
kind create cluster --name frkr-dev
kubectl cluster-info
```

**Note:** `frkrup` will deploy to your currently active kubectl context. It will not create a cluster for you.

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

**Alternative: Use a config file**

```bash
./bin/frkrup --config examples/config-kind.yaml
```

**Alternative: Use Makefile (for automation/CI)**

```bash
# Single command to build all images, setup cluster, and deploy
make kind-up deploy
```

**What happens:**
1. **Cluster Detection**: `frkrup` detects if kubectl is connected to a cluster
2. **Build & Load**: If `image_load_command` is configured, builds Docker images and loads them into the cluster
3. **Infrastructure**: Installs Gateway API CRDs, Envoy Gateway (if `external_access: ingress`), and cert-manager (if `install_cert_manager: true`)
4. **Helm Chart**: Deploys the frkr Helm chart (Operator, Gateways, routes)
5. **Readiness**: Waits for pods and migration jobs to complete
6. **Port Forwarding**: If configured, sets up port forwarding for local access

**Verification:**
- Ingest Gateway: `http://localhost:8082/health` (or port you configured)
- Streaming Gateway: `http://localhost:8081/health` (or port you configured)

### Local Image Loading

When running a local cluster without a container registry, you need a way to get locally-built Docker images into the cluster's container runtime. Set `image_load_command` in your config so frkrup can build and load images automatically.

The image name is appended as an argument to the command.

| Cluster Tool | `image_load_command` |
|---|---|
| Kind | `kind load docker-image --name <cluster-name>` |
| minikube | `minikube image load` |
| k3d | `k3d image import` |

Example config:
```yaml
k8s: true
image_load_command: "kind load docker-image --name frkr-dev"
```

If you are using a container registry (local or remote), set `image_registry` instead and omit `image_load_command`. The standard build/push/pull flow works with any cluster type.

### Production Deployments

When deploying to a managed Kubernetes cluster (e.g., EKS, GKE, AKS):
1. Set `skip_port_forward: true`
2. Set `image_registry` to your cloud container registry
3. Configure external access via `external_access: ingress`:
   - **Single hostname**: All traffic goes through Envoy Gateway on one hostname. HTTP endpoints (`/health`, `/ingest`, `/metrics`) route to the ingest gateway. gRPC traffic routes to the streaming gateway.
   - **Per-service subdomains**: Same Envoy Gateway, but each service gets its own hostname (e.g., `ingest.frkr.example.com` and `stream.frkr.example.com`)
   - **None**: ClusterIP only (internal access, no external exposure)
4. See [TLS Setup](docs/TLS-SETUP.md) for HTTPS configuration.

---

## Step 4: Configure Stream & User

Since `frkr` is secure by default, you need to create a stream and a user to access it.

**In a new terminal:**

```bash
cd frkr-tools

# Build the CLI tools if needed
make build

# 0. Create a Tenant (Required)
# Use frkrctl to create a tenant (managed via Kubernetes CRD)
# The default tenant is named 'default'.
./bin/frkrctl tenant create default

# Get the Tenant ID (from the K8s CRD status)
# Wait a few seconds for the tenant to be ready
export TENANT_ID=$(./bin/frkrctl tenant get default)
echo "Tenant ID: $TENANT_ID"

# 1. Create a Stream (via Kubernetes Operator)
# This creates a FrkrStream Custom Resource
./bin/frkrctl stream create my-api --tenant-id $TENANT_ID

# 2. Create a User (via Kubernetes Operator)
# This creates a FrkrUser Custom Resource.
./bin/frkrctl user create testuser --tenant-id $TENANT_ID

# The auto-generated password will be displayed in the output.
# SAVE IT! It will not be shown again.
# Password: <generated-password>
export PASSWORD=<generated-password>
```

---

## Step 5: Start Example API

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
  --gateway=localhost:8081 \
  --username=testuser \
  --password=$PASSWORD \
  --insecure \
  --forward-url=http://localhost:3001
```

Watch the `frkr-example-api` terminal - you'll see mirrored requests labeled as **FORWARDED FROM FRKR**.

---

## Provisioning Users and Credentials

### Create Users for CLI Authentication

To create additional users for CLI access:

```bash
cd frkr-tools

# Use the Tenant ID from Step 4
export TENANT_ID="<your-uuid-here>"

./bin/frkrctl user create another-user --tenant-id $TENANT_ID
```

**Note:** Save the password! It won't be shown again.


You can then use these credentials with the CLI:
```bash
./bin/frkr stream my-api \
  --gateway=localhost:8081 \
  --username=streamuser \
  --password=your-secure-password \
  --insecure \
  --forward-url=http://localhost:3001
```

### Create Client Credentials for SDK Authentication

For SDK clients that need to authenticate with client ID/secret, use `frkrctl` with port-forwarding:

```bash
cd frkr-tools

# Create a client credential (secret will be auto-generated if not provided)
./bin/frkrctl client create my-sdk-client --tenant-id <TENANT_ID>

# Optionally scope the client to a specific stream
./bin/frkrctl client create my-stream-client \
  --tenant-id <TENANT_ID> \
  --stream-id <STREAM_ID>

# Or provide your own secret
./bin/frkrctl client create my-byo-client \
  --tenant-id <TENANT_ID> \
  --secret "your-client-secret-here"
```

**Note:**
- Save the client secret shown - it won't be displayed again!
- Stream scoping is optional - clients without a stream can access all streams for the tenant
- The default tenant name is `default` - use `--tenant` flag to specify a different tenant

**List existing clients:**
```bash
./bin/frkrctl client list
```

**Use the client credentials in your SDK:**
```javascript
// Node.js SDK example
const frkr = require('@frkr-io/sdk-node');

const client = new frkr.Client({
  gatewayUrl: 'http://localhost:8082',
  clientId: 'my-sdk-client',
  clientSecret: 'your-client-secret-here',
  streamId: 'my-api'
});
```

---

## What's Next?

- **Set up authentication**: See [Provisioning Users and Credentials](#provisioning-users-and-credentials) above to create real users for CLI access and client credentials for SDK authentication (instead of using testuser/testpass)
- **Route-based routing**: See [Node SDK README](https://github.com/frkr-io/frkr-sdk-node/README.md#route-based-stream-routing) for sending different routes to different streams
- **Local Docker setup**: See [Quick Start Guide](QUICKSTART.md) for Docker Compose setup
- **Full documentation**: See [README](README.md) for advanced usage

---

## Troubleshooting

**Cluster not found?**
```bash
# Verify your kubectl context is set correctly
kubectl config current-context
kubectl cluster-info
```

**Wrong kubectl context?**
```bash
# List all contexts
kubectl config get-contexts

# Switch to the correct context
kubectl config use-context <your-context>

# Verify connection
kubectl cluster-info
```

**Note:** `frkrup` uses the kubectl context from `~/.kube/config`, not terminal-specific environment variables. If `k8s_cluster_name` is set in your config, `frkrup` will verify it matches the active context.

**Port forwarding:**
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
- Verify port forwarding is active (if using port forwarding)
- Check Ingress/Envoy IPs: `kubectl get svc -n envoy-gateway-system`
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

# Delete the cluster (if using Kind)
kind delete cluster --name frkr-dev

# Recreate and restart from Step 1
```

---

## Cleanup

To completely remove frkr from your cluster:

```bash
# Stop frkrup (Ctrl+C) to stop port forwarding

# Uninstall Helm release
helm uninstall frkr

# Optional: Delete the cluster entirely
# (command depends on your cluster tool)
```
