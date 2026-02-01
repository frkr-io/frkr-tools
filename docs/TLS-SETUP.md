# Production TLS Setup

This guide explains how to secure your `frkr` deployment with HTTPS/TLS using `cert-manager` and `frkrup`.

## Prerequisites

- A deployed `frkr` cluster (on Azure, OCI, DigitalOcean, or others).
- `frkrup` installed.
- The LoadBalancer IP address of your cluster (output by `frkrup` or `kubectl get svc -A`).

---

## 1. Configure Config (`frkrup.yaml`)

To enable TLS, you must switch `frkrup` from simple LoadBalancer mode to **Ingress** mode and enable `cert-manager`.

Update your config file:

```yaml
# 1. Switch access mode
external_access: ingress

# 2. Define your hostname
# Option A: Magic DNS (if you don't have a domain)
# Replace dashes with your IP: 1.2.3.4 -> 1-2-3-4.sslip.io
ingress_host: "1-2-3-4.sslip.io"

# Option B: Custom Domain
# ingress_host: "api.example.com"

# 3. Configure TLS
ingress_tls_secret: frkr-tls  # Name of secret to create
install_cert_manager: true
cert_manager_email: "your-email@example.com" # Required for Let's Encrypt
```

---

## 2. Option A: Magic DNS (sslip.io)

This is the fastest method. It uses a wildcard DNS service that resolves to the IP address in the hostname.

1.  **Get IP**: Run `kubectl get svc -n default` and look for the `LoadBalancer` Service IP.
2.  **Format Hostname**: Convert dots to dashes and append `.sslip.io`.
    *   Example: `203.0.113.1` -> `203-0-113-1.sslip.io`.
3.  **Apply**:
    ```bash
    frkrup --config your-config.yaml
    ```

**Result**: You can verify immediately:
```bash
curl -v https://203-0-113-1.sslip.io/health
```

---

## 3. Option B: Custom Domain

1.  **DNS**: Log in to your DNS provider (Cloudflare, GoDaddy, etc.).
2.  **Create Record**: Create an **A Record** pointing your subdomain (e.g., `api`) to the cluster's LoadBalancer IP.
3.  **Update Config**: Set `ingress_host: api.yourdomain.com`.
4.  **Apply**:
    ```bash
    frkrup --config your-config.yaml
    ```

---

## Troubleshooting

### Certificate not issued?
Check the Cert Manager status:

```bash
kubectl get certificaterequest -A
kubectl get challenges -A
kubectl describe clusterissuer letsencrypt-prod
```

### 502 Bad Gateway?
This usually means the Ingress Controller (Envoy) is running, but can't reach the `frkr` gateways. Check `kubectl get pods`.
