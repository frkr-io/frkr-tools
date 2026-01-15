# Cloud Kubernetes Ingress Setup

This guide covers how to expose frkr gateways to external traffic in a production Kubernetes environment.

## Overview

frkr consists of two gateways that need external access:
- **Ingest Gateway** (HTTP) — Receives mirrored traffic from your applications
- **Streaming Gateway** (gRPC) — Streams traffic to CLI consumers

## Ingress Options

| Method | Best For | TLS Termination |
|--------|----------|-----------------|
| **Cloud Load Balancer** | AWS/GCP/Azure managed K8s | At LB or gateway |
| **Ingress Controller** | Self-hosted, multi-tenant | At ingress controller |
| **Service Mesh** | Istio/Linkerd environments | At sidecar |

---

## Option 1: Cloud Load Balancer (AWS/GCP/Azure)

### AWS (EKS with NLB)

```yaml
# values.yaml for frkr Helm chart
ingestGateway:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      # For TLS termination at NLB:
      service.beta.kubernetes.io/aws-load-balancer-ssl-cert: "arn:aws:acm:..."
      service.beta.kubernetes.io/aws-load-balancer-ssl-ports: "443"

streamingGateway:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
```

### GCP (GKE)

```yaml
ingestGateway:
  service:
    type: LoadBalancer
    annotations:
      networking.gke.io/load-balancer-type: "External"

streamingGateway:
  service:
    type: LoadBalancer
```

### Azure (AKS)

```yaml
ingestGateway:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/azure-load-balancer-resource-group: "my-rg"
```

---

## Option 2: Kubernetes Ingress Controller

### nginx Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: frkr-ingress
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - ingest.frkr.example.com
      secretName: frkr-tls
  rules:
    - host: ingest.frkr.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: frkr-ingest-gateway
                port:
                  number: 8082
```

> **Note**: For the Streaming Gateway (gRPC), you need an ingress controller that supports gRPC (e.g., nginx with `nginx.ingress.kubernetes.io/backend-protocol: "GRPC"`).

### Traefik

```yaml
apiVersion: traefik.containo.us/v1alpha1
kind: IngressRoute
metadata:
  name: frkr-ingest
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`ingest.frkr.example.com`)
      kind: Rule
      services:
        - name: frkr-ingest-gateway
          port: 8082
  tls:
    certResolver: letsencrypt
```

---

## Option 3: BYO (Bring Your Own) Load Balancer

If you're using an external load balancer (F5, HAProxy, etc.) that terminates TLS:

### 1. Configure ClusterIP Services

```yaml
# values.yaml
ingestGateway:
  service:
    type: ClusterIP
streamingGateway:
  service:
    type: ClusterIP
```

### 2. Expose via NodePort (if LB can't route to ClusterIP)

```yaml
ingestGateway:
  service:
    type: NodePort
    nodePort: 30082  # External LB targets this port
```

### 3. Configure Trusted Headers

When TLS is terminated upstream, frkr gateways accept forwarded headers:

| Header | Purpose |
|--------|---------|
| `X-Forwarded-For` | Client IP |
| `X-Forwarded-Proto` | Original protocol (https) |
| `X-Real-IP` | Alternative client IP header |

Ensure your LB sets these headers correctly.

---

## TLS Configuration

### Gateway-Terminated TLS

If gateways terminate TLS directly (no upstream termination):

```yaml
ingestGateway:
  tls:
    enabled: true
    secretName: frkr-ingest-tls

streamingGateway:
  tls:
    enabled: true
    secretName: frkr-streaming-tls
```

Create the secrets:

```bash
kubectl create secret tls frkr-ingest-tls \
  --cert=ingest.crt --key=ingest.key -n frkr

kubectl create secret tls frkr-streaming-tls \
  --cert=streaming.crt --key=streaming.key -n frkr
```

### Upstream-Terminated TLS

If your LB/ingress terminates TLS, gateways can run in plaintext mode:

```yaml
ingestGateway:
  tls:
    enabled: false
```

> **Security Note**: Ensure internal cluster traffic is secure (e.g., via network policies or service mesh mTLS).

---

## DNS Configuration

After deployment, configure DNS records:

```
ingest.frkr.example.com  → Load Balancer IP (Ingest Gateway)
stream.frkr.example.com  → Load Balancer IP (Streaming Gateway)
```

---

## Verification

### Check Service Endpoints

```bash
kubectl get svc -n frkr
kubectl get endpoints -n frkr
```

### Test Connectivity

```bash
# Ingest Gateway (HTTP)
curl -v https://ingest.frkr.example.com/health

# Streaming Gateway (gRPC)
grpcurl -plaintext stream.frkr.example.com:443 grpc.health.v1.Health/Check
```

### Check Metrics

```bash
curl https://ingest.frkr.example.com/metrics
```

---

## Troubleshooting

| Issue | Check |
|-------|-------|
| 502 Bad Gateway | Pod readiness, service selector |
| TLS handshake failed | Certificate chain, SNI config |
| gRPC connection refused | Backend protocol annotation |
| No external IP | Cloud LB provisioning status |

```bash
# View gateway logs
kubectl logs -l app.kubernetes.io/name=frkr-ingest-gateway -n frkr

# Check LB status (AWS)
aws elbv2 describe-load-balancers --query 'LoadBalancers[?contains(LoadBalancerName, `frkr`)]'
```
