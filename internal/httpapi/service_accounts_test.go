package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	fastcas "github.com/FastR-D/FastCAS/sdk/go"
)

func TestAdminServiceAccountHTTPAndTokenLifecycle(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	response, _ := b.request("GET", "/api/v1/admin/service-accounts", nil)
	if response.StatusCode != 401 {
		t.Fatalf("anonymous service list: %d", response.StatusCode)
	}
	admin, err := store.CreateIdentity(ctx, "admin@example.test", "Admin", "test-admin-password-long-enough", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, err := store.NewSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE identities SET mfa_secret='\x01'::bytea WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE identity_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any, origin, csrfValue string) (int, []byte) {
		t.Helper()
		var raw io.Reader
		if body != nil {
			data, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			raw = bytes.NewReader(data)
		}
		req, e := http.NewRequest(method, b.issuer+path, raw)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Cookie", "fastcas_session="+token)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrfValue)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		r, e := b.http.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		data, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		return r.StatusCode, data
	}
	create := map[string]any{"id": "pipeline", "name": "Pipeline", "scopes": []string{"insight:publish"}, "resources": []string{"research-api"}}
	if status, _ := request("POST", "/api/v1/admin/service-accounts", create, "https://attacker.test", csrf); status != 403 {
		t.Fatalf("cross-origin creation: %d", status)
	}
	if status, _ := request("POST", "/api/v1/admin/service-accounts", create, b.issuer, ""); status != 403 {
		t.Fatalf("missing CSRF: %d", status)
	}
	status, data := request("POST", "/api/v1/admin/service-accounts", create, b.issuer, csrf)
	if status != 201 {
		t.Fatalf("create service: %d %s", status, data)
	}
	var fields map[string]string
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	clientID, secret := fields["id"], fields["client_secret"]
	if clientID != "pipeline" || len(secret) < 32 {
		t.Fatal("missing one-time service secret")
	}
	if err = store.RegisterClient(ctx, core.Client{ID: "research-api", Name: "Research resource", Development: true, Redirects: []string{"http://127.0.0.1:3401/callback"}, Scopes: []string{"openid"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	sdk, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: clientID, ClientSecret: secret, RedirectURI: "http://127.0.0.1:3400/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	resourceSDK, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: "research-api", ClientSecret: clientSecret, RedirectURI: "http://127.0.0.1:3401/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	sdkToken, err := sdk.ClientCredentials(ctx, []string{"insight:publish"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sdk.VerifyAccessToken(ctx, sdkToken.AccessToken, "research-api", "insight:publish"); err != nil {
		t.Fatal(err)
	}
	if live, err := resourceSDK.IntrospectToken(ctx, sdkToken.AccessToken); err != nil || !live.Active || live.ClientID != clientID {
		t.Fatalf("Go SDK service introspection before disable: %+v %v", live, err)
	}
	issue := func(credential string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest("POST", b.issuer+"/oauth/token", strings.NewReader("grant_type=client_credentials&scope=insight%3Apublish"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth("pipeline", credential)
		response, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body map[string]any
		if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	if status, body := issue(secret); status != 200 || body["access_token"] == nil || body["refresh_token"] != nil {
		t.Fatalf("service token: %d %+v", status, body)
	}
	status, data = request("GET", "/api/v1/admin/service-accounts", nil, b.issuer, csrf)
	if status != 200 || strings.Contains(string(data), secret) {
		t.Fatalf("service list leaked secret: %d", status)
	}
	var listed []core.ServiceAccount
	if err = json.Unmarshal(data, &listed); err != nil || len(listed) != 1 || listed[0].Client.ID != "pipeline" {
		t.Fatalf("service list: %+v %v", listed, err)
	}
	if status, _ = request("POST", "/api/v1/admin/service-accounts/pipeline/status", map[string]bool{"active": false}, b.issuer, csrf); status != 200 {
		t.Fatalf("disable service: %d", status)
	}
	if live, err := resourceSDK.IntrospectToken(ctx, sdkToken.AccessToken); err != nil || live.Active {
		t.Fatalf("Go SDK service introspection after disable: %+v %v", live, err)
	}
	if err = store.AuthorizeClientIDSecret(ctx, "pipeline", secret); err == nil {
		t.Fatal("disabled secret accepted")
	}
	if status, _ := issue(secret); status == 200 {
		t.Fatal("disabled service issued token")
	}
	status, data = request("POST", "/api/v1/admin/service-accounts/pipeline/status", map[string]bool{"active": true}, b.issuer, csrf)
	if status != 200 {
		t.Fatalf("enable service: %d %s", status, data)
	}
	var enabled struct {
		ClientSecret string `json:"client_secret"`
	}
	if err = json.Unmarshal(data, &enabled); err != nil {
		t.Fatal(err)
	}
	if enabled.ClientSecret == "" || enabled.ClientSecret == secret {
		t.Fatal("re-enable did not rotate secret")
	}
	if err = store.AuthorizeClientIDSecret(ctx, "pipeline", enabled.ClientSecret); err != nil {
		t.Fatal(err)
	}
	if status, _ := issue(enabled.ClientSecret); status != 200 {
		t.Fatalf("re-enabled service token status %d", status)
	}
	status, data = request("POST", "/api/v1/admin/service-accounts/pipeline/rotate-secret", map[string]any{}, b.issuer, csrf)
	if status != 200 {
		t.Fatalf("rotate service secret: %d %s", status, data)
	}
	var rotated struct {
		ClientSecret string `json:"client_secret"`
	}
	if err = json.Unmarshal(data, &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.ClientSecret == "" || rotated.ClientSecret == enabled.ClientSecret {
		t.Fatal("rotation did not issue a new secret")
	}
	if status, _ := issue(rotated.ClientSecret); status != 200 {
		t.Fatalf("rotated service token status %d", status)
	}
}
