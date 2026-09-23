package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestDeviceAuthorizationSingleUseAndBrowserConfirmation(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	if err := store.RegisterClient(ctx, core.Client{ID: "fastlab", Name: "FastLab desktop", Public: true,
		Scopes: []string{"openid", "profile"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
		t.Fatal(err)
	}
	response, body := b.request("GET", "/.well-known/openid-configuration", nil)
	var discovery map[string]any
	if response.StatusCode != 200 || json.Unmarshal(body, &discovery) != nil || discovery["device_authorization_endpoint"] != b.issuer+"/oauth/device_authorization" {
		t.Fatalf("device discovery: %d %s", response.StatusCode, body)
	}
	response, body = b.request("POST", "/oauth/device_authorization", url.Values{"client_id": {"fastlab"}, "scope": {"openid profile"}})
	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if response.StatusCode != 200 || json.Unmarshal(body, &start) != nil || start.DeviceCode == "" || start.UserCode == "" || start.VerificationURI != b.issuer+"/device" || start.Interval != 5 {
		t.Fatalf("device start: %d %s", response.StatusCode, body)
	}
	poll := url.Values{"client_id": {"fastlab"}, "grant_type": {string(oidc.GrantTypeDeviceCode)}, "device_code": {start.DeviceCode}}
	response, body = b.request("POST", "/oauth/token", poll)
	if response.StatusCode != 400 || !strings.Contains(string(body), "authorization_pending") {
		t.Fatalf("premature poll: %d %s", response.StatusCode, body)
	}
	response, body = b.request("POST", "/oauth/token", poll)
	if response.StatusCode != 400 || !strings.Contains(string(body), "slow_down") {
		t.Fatalf("fast poll: %d %s", response.StatusCode, body)
	}
	response, body = b.request("GET", "/device?user_code="+url.QueryEscape(start.UserCode), nil)
	if response.StatusCode != 200 || !strings.Contains(string(body), "return_device="+start.UserCode) {
		t.Fatalf("device login handoff: %d %s", response.StatusCode, body)
	}
	_, loginPage := b.request("GET", "/login?return_device="+url.QueryEscape(start.UserCode), nil)
	response, body = b.request("POST", "/login", url.Values{"csrf": {csrf(t, loginPage)}, "return_device": {start.UserCode}, "email": {"alice@example.test"}, "password": {"correct horse battery staple"}, "action": {"login"}})
	if response.StatusCode != 303 || response.Header.Get("Location") != "/device?user_code="+start.UserCode {
		t.Fatalf("login: %d %s", response.StatusCode, body)
	}
	response, body = b.request("GET", "/device?user_code="+url.QueryEscape(start.UserCode), nil)
	if response.StatusCode != 200 || !strings.Contains(string(body), "FastLab desktop") {
		t.Fatalf("device confirmation page: %d %s", response.StatusCode, body)
	}
	confirm := url.Values{"csrf": {csrf(t, body)}, "user_code": {start.UserCode}, "decision": {"approve"}}
	wrong, _ := http.NewRequest("POST", b.issuer+"/device", strings.NewReader(confirm.Encode()))
	wrong.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrong.Header.Set("Origin", "https://attacker.example")
	result, err := b.http.Do(wrong)
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
	if result.StatusCode != 403 {
		t.Fatal("cross-origin device approval accepted")
	}
	response, body = b.request("POST", "/device", confirm)
	if response.StatusCode != 200 {
		t.Fatalf("approval: %d %s", response.StatusCode, body)
	}
	_, err = store.DB.Exec(ctx, `UPDATE device_authorizations SET next_poll_at=now()-interval '1 second' WHERE device_code_hash=$1`, core.Hash(start.DeviceCode))
	if err != nil {
		t.Fatal(err)
	}
	response, body = b.request("POST", "/oauth/token", poll)
	var tokens map[string]any
	if response.StatusCode != 200 || json.Unmarshal(body, &tokens) != nil || tokens["access_token"] == nil || tokens["refresh_token"] != nil {
		t.Fatalf("device token: %d %s", response.StatusCode, body)
	}
	parsed, err := jwt.ParseSigned(tokens["access_token"].(string), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		ID       string   `json:"jti"`
		Subject  string   `json:"sub"`
		Audience []string `json:"aud"`
		Service  bool     `json:"service"`
	}
	if err = parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Subject == "fastlab" || claims.Service || len(claims.Audience) != 1 || claims.Audience[0] != "fastlab" {
		t.Fatalf("invalid device claims: %+v", claims)
	}
	deviceToken := tokens["access_token"].(string)
	installRef := "550e8400-e29b-41d4-a716-446655440000"
	register := func(token, ref string) (*http.Response, []byte) {
		t.Helper()
		request, err := http.NewRequest("POST", b.issuer+"/api/v1/device-installations", bytes.NewBufferString(`{"installation_id":"`+ref+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := b.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}
	response, _ = register("invalid", installRef)
	if response.StatusCode != 401 {
		t.Fatal("unsigned installation registration accepted")
	}
	response, body = register(deviceToken, installRef)
	var installation struct {
		Installation     core.DeviceInstallation `json:"installation"`
		ManagementSecret string                  `json:"management_secret"`
	}
	if response.StatusCode != 201 || json.Unmarshal(body, &installation) != nil || installation.Installation.InstallationRef != installRef || len(installation.ManagementSecret) < 32 {
		t.Fatalf("installation registration: %d %s", response.StatusCode, body)
	}
	response, _ = register(deviceToken, "550e8400-e29b-41d4-a716-446655440001")
	if response.StatusCode != 409 {
		t.Fatal("device token registered two installations")
	}
	response, body = b.request("GET", "/api/v1/me/device-installations", nil)
	var listed struct {
		Installations []core.DeviceInstallation `json:"installations"`
		NextCursor    string                    `json:"next_cursor"`
	}
	if response.StatusCode != 200 || json.Unmarshal(body, &listed) != nil || len(listed.Installations) != 1 || listed.Installations[0].ID != installation.Installation.ID || listed.NextCursor != "" {
		t.Fatalf("installation list: %d %s", response.StatusCode, body)
	}
	other, err := store.CreateIdentity(ctx, "other@example.test", "Other", "other correct horse battery", "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDeviceInstallation(ctx, installation.Installation.ID, other.ID); err != core.ErrForbidden {
		t.Fatalf("another identity could revoke installation: %v", err)
	}
	wrongSecret, _ := http.NewRequest("GET", b.issuer+"/api/v1/device-installations/"+installation.Installation.ID, nil)
	wrongSecret.Header.Set("Authorization", "Bearer "+core.RandomToken())
	denied, err := b.http.Do(wrongSecret)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatalf("wrong installation secret got %d", denied.StatusCode)
	}
	request, _ := http.NewRequest("GET", b.issuer+"/api/v1/device-installations/"+installation.Installation.ID, nil)
	request.Header.Set("Authorization", "Bearer "+installation.ManagementSecret)
	result, err = b.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
	if result.StatusCode != 200 {
		t.Fatal("installation secret could not read own status")
	}
	response, body = b.request("GET", "/api/v1/me", nil)
	var me struct {
		CSRF string `json:"csrf"`
	}
	if response.StatusCode != 200 || json.Unmarshal(body, &me) != nil {
		t.Fatal("missing browser CSRF")
	}
	request, _ = http.NewRequest("POST", b.issuer+"/api/v1/me/device-installations/"+installation.Installation.ID+"/revoke", bytes.NewBufferString(`{}`))
	request.Header.Set("Origin", b.issuer)
	request.Header.Set("X-CSRF-Token", me.CSRF)
	request.Header.Set("Content-Type", "application/json")
	result, err = b.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
	if result.StatusCode != 204 {
		t.Fatalf("browser installation revoke: %d", result.StatusCode)
	}
	item, err := store.DeviceInstallationBySecret(ctx, installation.Installation.ID, installation.ManagementSecret)
	if err != nil || item.RevokedAt == nil {
		t.Fatalf("revoked installation still active: %+v %v", item, err)
	}
	if _, err := store.ActiveToken(ctx, claims.ID); err != core.ErrUnauthorized {
		t.Fatalf("installation revoke left device access token active: %v", err)
	}
	if _, err := store.DeviceAccessToken(ctx, deviceToken, b.issuer); err != core.ErrUnauthorized {
		t.Fatalf("installation revoke accepted old bearer: %v", err)
	}
	if _, err := store.DB.Exec(ctx, `UPDATE access_tokens SET revoked_at=NULL WHERE id=$1`, claims.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDeviceInstallationWithSecret(ctx, installation.Installation.ID, installation.ManagementSecret); err != nil {
		t.Fatalf("idempotent secret revoke failed: %v", err)
	}
	if _, err := store.ActiveToken(ctx, claims.ID); err != core.ErrUnauthorized {
		t.Fatalf("idempotent revoke left device access token active: %v", err)
	}
	response, body = b.request("POST", "/oauth/token", poll)
	if response.StatusCode == 200 {
		t.Fatalf("device code replay accepted: %s", body)
	}
}
