package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestOIDCVerificaton verifies that the OIDC setup is working correctly.
// This test expects frkrup to be running with OIDC enabled on localhost.
func TestOIDCVerification(t *testing.T) {
	t.Log("🔍 Verifying OIDC Setup...")

	// 1. Check Mock OIDC
	resp, err := http.Get("http://localhost:8085/default/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("❌ Failed to reach Mock OIDC: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("❌ Mock OIDC returned status %d", resp.StatusCode)
	}
	t.Log("✅ Mock OIDC is running and accessible")

	// 2. Get Token
	t.Log("🔑 Fetching Access Token...")
	tokenURL := "http://localhost:8085/default/token"
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", "test-client")
	data.Set("client_secret", "test-secret")
	data.Set("audience", "default")

	resp, err = http.PostForm(tokenURL, data)
	if err != nil {
		t.Fatalf("❌ Failed to fetch token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("❌ Failed to fetch token (Status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("❌ Failed to decode token response: %v", err)
	}
	t.Log("✅ Access Token obtained")

	// 3. Verify Gateway Authentication
	
	// Test Case A: No Token -> Should be 401
	t.Log("🛡️  Testing Unauthenticated Access (expecting 401)...")
	req, _ := http.NewRequest("POST", "http://localhost:8082/ingest", strings.NewReader("{}"))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("❌ Failed to call gateway: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == 401 {
			t.Log("✅ Unauthenticated request rejected (401)")
		} else {
			t.Errorf("❌ Unexpected status for unauthenticated request: %d (expected 401)", resp.StatusCode)
		}
	}

	// Test Case B: With Token -> Should be Success (200/202) or 404 (if stream missing), NOT 401
	t.Log("🛡️  Testing Authenticated Access (expecting success/404/400)...")
	req, _ = http.NewRequest("POST", "http://localhost:8082/ingest", strings.NewReader(`{"valid":"json"}`))
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("❌ Failed to call gateway: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		t.Fatal("❌ Authenticated request REJECTED (401) - OIDC setup failed!")
	} else if resp.StatusCode == 404 {
		t.Log("✅ Authenticated request accepted (404 Stream Not Found is expected)")
	} else if resp.StatusCode == 200 || resp.StatusCode == 202 {
		t.Log("✅ Authenticated request accepted (200/202)")
	} else if resp.StatusCode == 400 {
		t.Log("✅ Authenticated request accepted (400 Bad Request is expected for invalid body)")
	} else {
		t.Logf("⚠️  Authenticated request returned status %d", resp.StatusCode)
	}

	t.Log("🎉 OIDC Verification Successful!")
}

