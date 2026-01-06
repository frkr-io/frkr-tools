# Kubernetes AuthN/Z Implementation Guide (2026)

This guide provides a complete, vendor-agnostic implementation for securing Kubernetes services using **Envoy Gateway** and **Auth0** (or any OIDC provider).

### 1. Infrastructure: Envoy Gateway SecurityPolicy
This configuration offloads all JWT validation to the edge. Your backend services will only receive requests that have already been cryptographically verified against Auth0's public keys.

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: auth0-jwt-protection
  namespace: envoy-gateway-system
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: my-service-route
  jwt:
    providers:
      - name: auth0
        issuer: https://YOUR_domain.auth0.com
        remoteJwks:
          uri: https://YOUR_domain.auth0.com.well-known/jwks.json
        # The 'audience' must match your Auth0 API Identifier
        audiences: 
          - https://api.myapp.com 
```

### 2. Console App: Go (PKCE Flow)
This client-side implementation uses standard libraries to perform the Authorization Code Flow with PKCE. It avoids vendor-specific SDKs.

**Dependencies:** `go get golang.org/x/oauth2 github.com`

```go
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com"
	"golang.org/x/oauth2"
)

func main() {
	token, err := loginWithPKCE()
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		return
	}
	fmt.Printf("Successfully Authenticated!\nAccess Token: %s\n", token.AccessToken)
}

func loginWithPKCE() (*oauth2.Token, error) {
	ctx := context.Background()
	
	// Generic OIDC Configuration
	conf := &oauth2.Config{
		ClientID: "YOUR_AUTH0_CLIENT_ID",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://YOUR_domain.auth0.comauthorize",
			TokenURL: "https://YOUR_domain.auth0.comoauth/token",
		},
		RedirectURL: "http://localhost:8080/callback",
		Scopes:      []string{"openid", "profile", "email"},
	}

	// 1. Generate PKCE Verifier and Challenge
	verifier := generateRandomString(64)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// 2. Setup local server to capture the redirect code
	codeChan := make(chan string)
	server := &http.Server{Addr: ":8080"}
	
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		codeChan <- code
		fmt.Fprint(w, "Login successful! You may now close this tab and return to the terminal.")
	})

	go server.ListenAndServe()

	// 3. Build Auth URL and open browser
	// Note: 'audience' is required by Auth0 to return a valid JWT Access Token
	url := conf.AuthCodeURL("random-state-string",
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("audience", "https://api.myapp.com"),
	)
	
	fmt.Println("Opening browser for login...")
	browser.OpenURL(url)

	// 4. Wait for code, then exchange for Access Token
	code := <-codeChan
	server.Shutdown(ctx)

	return conf.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
```

### 3. Server-Side SDK: Node.js (M2M Flow)
For a service to call your API securely from another server, it uses the Client Credentials grant.

**Dependencies:** `npm install openid-client`

```javascript
const { Issuer } = require('openid-client');

async function getAccessToken() {
  // 1. Automatically discover OIDC endpoints from Auth0
  const auth0 = await Issuer.discover('https://YOUR_domain.auth0.com');
  
  const client = new auth0.Client({
    client_id: 'YOUR_M2M_CLIENT_ID',
    client_secret: 'YOUR_M2M_CLIENT_SECRET'
  });

  // 2. Perform the Machine-to-Machine grant
  const tokenSet = await client.grant({
    grant_type: 'client_credentials',
    audience: 'https://api.myapp.com'
  });

  console.log('Received JWT Access Token:', tokenSet.access_token);
  return tokenSet.access_token;
}

// Example usage:
// const token = await getAccessToken();
// axios.get('k8s-service.com', { headers: { Authorization: `Bearer ${token}` } });
```

### 4. Key Takeaways for 2026
*   **SecurityPolicy > App Code:** Your Go API behind Envoy does not need any OIDC libraries. Envoy validates the signature, issuer, and expiration, passing only "clean" requests to your app.
*   **PKCE for CLIs:** Never use Client Secrets in Go CLI tools. Always use PKCE (Proof Key for Code Exchange) as shown in the Go example.
*   **Standardized Libraries:** By using `golang.org/x/oauth2` and `openid-client`, you are not locked into Auth0. Switching to Okta or self-hosted Keycloak only requires updating the URLs.
