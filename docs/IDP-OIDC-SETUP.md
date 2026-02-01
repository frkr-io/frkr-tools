# Identity Provider (IdP) OIDC Setup

This guide covers how to configure popular managed Identity Providers for use with frkr.

## Overview

frkr requires OIDC for two primary purposes:
1. **Gateway Auth** — Gateways verify OIDC tokens (JWTs) from applications/consumers.
2. **CLI Auth** — The `frkr login` command uses the PKCE flow to authenticate users.

### Common Metadata Requirements
Regardless of the provider, you will need:
- **Issuer URL**: The base URL of the IdP (e.g., `https://dev-xxx.auth0.com/`)
- **Client ID**: A unique identifier for the frkr application.
- **Client Secret**: Required for the gateways (backends), NOT for the CLI.

---

## Option 1: Auth0 (Developer Friendly)

### 1. Create an Application
- Go to **Applications** > **Applications** > **Create Application**.
- Select **Native** for the CLI or **Regular Web App** for the Gateways.

### 2. Configure Settings
- **Allowed Callback URLs**: `http://localhost:38911/callback` (for `frkr login`).
- **Allowed Logout URLs**: `http://localhost:38911`.
- **Allowed Origins (CORS)**: `http://localhost:38911`.

### 3. frkr Config
```yaml
auth:
  type: oidc
  oidc:
    issuer: https://YOUR_DOMAIN.auth0.com/
    clientId: YOUR_CLIENT_ID
    # clientSecret: Only for gateways
```

---

## Option 2: Okta (Enterprise Standard)

### 1. Create App Integration
- Go to **Applications** > **Create App Integration**.
- Select **OIDC - OpenID Connect**.
- Application type: **Native** (for CLI) or **Web Application** (for Gateways).

### 2. Configure Grant Types
- Ensure **Authorization Code** and **Refresh Token** are enabled.
- **Sign-in redirect URIs**: `http://localhost:38911/callback`.
- **Sign-out redirect URIs**: `http://localhost:38911`.

### 3. API Authorization Server
- Okta often requires using a specific Authorization Server (e.g., `default`).
- Your issuer URL will look like: `https://YOUR_SUBDOMAIN.okta.com/oauth2/default`.

---

## Option 3: Google Cloud Identity / GWS

### 1. Create Credentials
- Go to **APIs & Services** > **Credentials** in GCP Console.
- Click **Create Credentials** > **OAuth client ID**.
- Application type: **Desktop app** (for CLI) or **Web application** (for Gateways).

### 2. Configure Consent Screen
- You must configure an OAuth Consent Screen and add the `openid`, `profile`, and `email` scopes.

### 3. Issuer URL
- Google's global issuer URL is: `https://accounts.google.com`.
- Note: Google uses specific discovery at `https://accounts.google.com/.well-known/openid-configuration`.

---

## Option 4: Microsoft Entra ID (Azure AD)

### 1. Register an Application
- Go to **Microsoft Entra ID** > **App registrations** > **New registration**.
- Name: `frkr-cli` (or similar).
- Supported account types: "Accounts in this organizational directory only" (Single Tenant).
- **Client ID**: Once created, go to the **Overview** blade. Copy the **Application (client) ID** (e.g., `12345678-abcd-ef00-1234-567890abcdef`). This is your `oidc_client_id`.

### 2. Configure Redirect URIs
- **Platform**: Select **Mobile and desktop applications**.
- **Redirect URIs**: Add `http://localhost:38911/callback` (Required for `frkr login`).
- **Web Platform**: If utilizing the Gateways for web-based flows, add a **Web** platform with your app's callback URL.

### 3. Certificates & Secrets
- Go to **Certificates & secrets** > **New client secret**.
- Copy the **Value** immediately (this is your `clientSecret`).

### 4. Issuer URL
- Go to **Overview** > **Endpoints**.
- Copy the **OpenID Connect metadata document** URL, but remove the `/.well-known...` suffix.
- It usually looks like: `https://login.microsoftonline.com/YOUR_TENANT_ID/v2.0`.

---

## SDK Integration (Client Credentials)

For background services and SDK integrations (e.g., Node.js middleware), use the **Client Credentials** flow. This allows your service to authenticate with the Ingest Gateway without user interaction.

### 1. Create a Machine-to-Machine (M2M) App
- **Auth0**: Create an "M2M Application".
- **Okta**: Create a "Service" (Machine-to-Machine) app.
- **Google**: Create a "Service Account".
- **Microsoft Entra ID (Azure)**:
    - Use the App Registration created in Option 4.
    - **Important**: For `client_credentials` flow, the scope MUST be `<YOUR_CLIENT_ID>/.default` (e.g., `api://<client-id>/.default` or just `<client-id>/.default`).
    - You may need to "Grant Admin Consent" in the Azure Portal for the requested API permissions.

> [!NOTE]
> **DOKS/AKS Users**: Infrastructure providers like DigitalOcean or Azure Kubernetes Service do not act as the IdP for your *application* data. You still use the IdP chosen above (Auth0, Okta, Entra ID) for SDK authentication.

### 2. Configure Scopes
- Ensure the app has the `ingest:write` scope if you use granular permissions.

### 3. Usage in SDK

```javascript
const frkr = require('@frkr-io/sdk-node');

frkr.init({
  ingestGatewayUrl: 'https://ingest.frkr.example.com',
  streamId: 'my-stream',
  auth: {
    type: 'oidc',
    issuer: 'https://your-idp.com/',
    clientId: 'YOUR_CLIENT_ID',
    clientSecret: 'YOUR_CLIENT_SECRET'
  }
});
```

---

## Configuring frkr Gateways

The gateways need to trust the IdP to verify incoming traffic. In your `values.yaml` or config:

```yaml
ingestGateway:
  auth:
    oidc:
      issuer: "https://your-idp.com/"
      audience: "your-client-id-or-api-identifier"
```

## Configuring frkr CLI

Run the login command with the provider details:

```bash
frkr login \
  --auth-url=https://your-idp.com/authorize \
  --token-url=https://your-idp.com/oauth/token \
  --client-id=YOUR_CLIENT_ID
```

> [!TIP]
> Most OIDC providers support a discovery endpoint. You can usually find the `auth-url` and `token-url` by visiting `<issuer-url>/.well-known/openid-configuration`.

---

## Production Deployment Configuration

For production deployments (e.g., on OCI or DOKS), you should inject these OIDC settings into your Helm values or `frkrup` configuration.

### frkrup Configuration (Recommended)

If you are using `frkrup` to deploy, simply add these keys to your `frkrup.yaml` (or config file):

```yaml
oidc_issuer: "https://idp.example.com/"
oidc_client_id: "YOUR_GATEWAY_CLIENT_ID"
oidc_client_secret: "YOUR_SECRET" # Optional
```

`frkrup` will automatically generate the corresponding Helm values below.

### Helm / Values.yaml (Manual)

If you are using Helm directly, update your `values.yaml`:

```yaml
global:
  auth:
    type: "oidc"
    oidc:
      issuer: "https://idp.example.com/"
      clientId: "YOUR_GATEWAY_CLIENT_ID"
      clientSecret: "YOUR_SECRET"

# Gateways Configuration (Verify Audience)
frkr-ingest-gateway:
  config: &gatewayConfig
    auth:
      enabled: true
      audience: "YOUR_GATEWAY_CLIENT_ID"

frkr-streaming-gateway:
  config: *gatewayConfig
```
