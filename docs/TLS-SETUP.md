# TLS Setup

Secure your `frkr` deployment with HTTPS/TLS.

## How it works

All external traffic enters through a single Envoy Gateway, which sits behind a cloud-provisioned L4 load balancer:

```
Client → Cloud L4 LB (auto-provisioned) → Envoy Gateway → Backend Services
```

Envoy terminates TLS and routes traffic by protocol:

| Route | Protocol | Backend |
|---|---|---|
| `/health`, `/ingest`, `/metrics` | HTTP | Ingest Gateway |
| All gRPC services | gRPC (HTTP/2) | Streaming Gateway |

There is one public IP. The cloud provisions the L4 load balancer automatically when the Envoy `Gateway` resource is created. For DDoS protection, attach your cloud provider's network-level protection to the VNet/VPC (e.g. Azure DDoS Protection Standard).

When `install_cert_manager: true` is set, `frkrup` will:
1. Install cert-manager as a standalone Helm release (in the `cert-manager` namespace).
2. Create a `ClusterIssuer` using Let's Encrypt ACME with HTTP-01 challenges routed through the Gateway.
3. Create a `Certificate` resource that requests the TLS cert and stores it in the configured secret.

---

## Configuration

All TLS-related settings go in your `frkrup.yaml`. There are two routing modes — pick one.

### Single hostname (path routing)

All traffic goes to one hostname. Envoy routes by path (HTTP) and protocol (gRPC).

```yaml
external_access: ingress
ingress_host: "frkr.example.com"

ingress_tls_secret: frkr-tls
install_cert_manager: true
cert_manager_email: "you@example.com"
```

### Per-service subdomains

Each service gets its own hostname. Useful when you need separate DNS records, certs, or CDN configs per service.

```yaml
external_access: ingress
ingest_ingress_host: "ingest.frkr.example.com"
streaming_ingress_host: "stream.frkr.example.com"

ingress_tls_secret: frkr-tls
install_cert_manager: true
cert_manager_email: "you@example.com"
```

> `ingress_host` and per-service hosts (`ingest_ingress_host`, `streaming_ingress_host`) are **mutually exclusive**. Config parsing will error if you mix them, or if you only set one of the two per-service hosts.

### Config reference

| Field | Required | Default | Description |
|---|---|---|---|
| `external_access` | yes | `none` | Set to `ingress` to enable Envoy Gateway routing. |
| `ingress_host` | if single-host | — | Hostname for path-based routing. |
| `ingest_ingress_host` | if subdomain | — | Hostname for the ingest service (subdomain routing). |
| `streaming_ingress_host` | if subdomain | — | Hostname for the streaming service (subdomain routing). |
| `ingress_tls_secret` | for TLS | — | Name of the Kubernetes TLS Secret that Envoy will use. |
| `install_cert_manager` | no | `false` | Install cert-manager for automatic Let's Encrypt certificates. |
| `cert_manager_email` | if cert-manager | — | Email for Let's Encrypt registration. |
| `cert_issuer_name` | no | `letsencrypt-prod` | ClusterIssuer name. |
| `cert_issuer_server` | no | Let's Encrypt prod URL | ACME server URL. Override to use staging (see below). |

---

## DNS options

### Option A: Real domain

1. Deploy first **without TLS** to get the external IP:
   ```yaml
   external_access: ingress
   ```
   ```bash
   bin/frkrup --config frkrup.yaml
   ```
2. Get the Envoy Gateway external IP:
   ```bash
   kubectl get svc -n envoy-gateway-system \
     -l gateway.envoyproxy.io/owning-gateway-name=frkr-gateway \
     -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}'
   ```
3. Create an A record at your DNS provider (Cloudflare, Route 53, etc.) pointing your hostname to that IP.
4. Add TLS settings to your config and re-run `frkrup`.

### Option B: sslip.io (no domain needed)

[sslip.io](https://sslip.io) is a free wildcard DNS service: any hostname containing an IP resolves to that IP. This lets you get a working TLS setup without owning a domain.

1. Deploy first without TLS to get the external IP (same as Option A).
2. Convert dots to dashes and append `.sslip.io`. For example, if the IP is `20.59.6.35`:
   ```
   20-59-6-35.sslip.io
   ```
3. Use that as your hostname with **Let's Encrypt staging**:
   ```yaml
   k8s: true
   k8s_cluster_name: my-aks-cluster
   external_access: ingress

   ingress_host: "20-59-6-35.sslip.io"
   ingress_tls_secret: frkr-tls
   install_cert_manager: true
   cert_manager_email: "you@example.com"
   cert_issuer_name: letsencrypt-staging
   cert_issuer_server: "https://acme-staging-v02.api.letsencrypt.org/directory"

   image_registry: "your-registry.azurecr.io"
   db_password: "your-password"
   ```
4. Run `frkrup --config frkrup.yaml`.

   For subdomain routing with sslip.io, use the same IP in both hostnames:
   ```yaml
   ingest_ingress_host: "ingest-20-59-6-35.sslip.io"
   streaming_ingress_host: "stream-20-59-6-35.sslip.io"
   ```
   Both resolve to the same IP, and Envoy routes by `Host` header.

> **Why staging?** sslip.io is a shared public domain. Let's Encrypt production enforces a rate limit of 50 certificates per registered domain per week. Because thousands of people use sslip.io, this limit is frequently exhausted. The staging server has much higher limits and is sufficient for development/testing. Staging certificates are not browser-trusted, but work fine with `--insecure` flags (see Verification below). Use Let's Encrypt production only with a real domain you own.

---

## Verification

After deploying, wait 1-2 minutes for cert-manager to complete the ACME challenge. Monitor with:

```bash
kubectl get certificate        # READY=True means cert is issued
kubectl get secret frkr-tls    # Should exist once cert is issued
```

### HTTP (Ingest Gateway)

```bash
# With a real (production) cert
curl -v https://YOUR-HOSTNAME/health

# With a staging cert (sslip.io)
curl -k https://YOUR-HOSTNAME/health

# Ingest endpoint
curl -X POST https://YOUR-HOSTNAME/ingest \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR-TOKEN" \
  -d '{"stream": "my-api", "method": "GET", "path": "/test"}'
```

### gRPC (Streaming Gateway)

```bash
# TLS with a real cert (port 443)
grpcurl YOUR-HOSTNAME:443 grpc.health.v1.Health/Check

# TLS with a staging cert (port 443)
grpcurl -insecure YOUR-HOSTNAME:443 grpc.health.v1.Health/Check

# Plaintext (port 80)
grpcurl -plaintext YOUR-HOSTNAME:80 grpc.health.v1.Health/Check

# List services
grpcurl -insecure YOUR-HOSTNAME:443 list
```

---

## Troubleshooting

**Certificate not issued?**

```bash
kubectl get certificate
kubectl get certificaterequest -A
kubectl get orders -A
kubectl get challenges -A
kubectl describe clusterissuer letsencrypt-prod   # or letsencrypt-staging
kubectl logs -n cert-manager deployment/cert-manager --tail=30
```

**Rate limit error (429)?**

```
too many certificates already issued for "sslip.io"
```

You're hitting Let's Encrypt production rate limits on a shared domain. Switch to staging:

```yaml
cert_issuer_name: letsencrypt-staging
cert_issuer_server: "https://acme-staging-v02.api.letsencrypt.org/directory"
```

Then delete the failed certificate and re-run:

```bash
kubectl delete certificate frkr-tls-cert
bin/frkrup --config frkrup.yaml
```

**Port 443 not responding / connection timeout?**

The HTTPS listener only activates once the TLS secret exists. Check if the cert has been issued:

```bash
kubectl get secret frkr-tls
kubectl get svc -n envoy-gateway-system   # Should show both 80 and 443
```

**502 Bad Gateway?**

Envoy is running but can't reach the backend pods:

```bash
kubectl get pods
kubectl logs -l gateway.envoyproxy.io/owning-gateway-name=frkr-gateway -n envoy-gateway-system
```

**External IP stuck on `<pending>`?**

```bash
kubectl get svc -n envoy-gateway-system
```

The cloud provider hasn't provisioned the load balancer yet. Check cloud-specific quotas and logs.
