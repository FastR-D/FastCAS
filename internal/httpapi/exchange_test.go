package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	fastcas "github.com/FastR-D/FastCAS/sdk/go"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestRestrictedTokenExchange(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	if _, err := store.DB.Exec(ctx, `UPDATE applications SET config=config || '{"grant_types":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:token-exchange"],"resources":["research-api"],"scopes":["openid","profile","email","offline_access","research:read"]}'::jsonb WHERE id='write'`); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterClient(ctx, core.Client{ID: "research", Name: "FastResearch", Development: true, Redirects: []string{"http://127.0.0.1:8787/callback"}, Scopes: []string{"openid", "research:read"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	var subject string
	if err := store.DB.QueryRow(ctx, `SELECT id FROM identities WHERE email='alice@example.test'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"write", "research"} {
		if _, err := store.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
		 VALUES($1,$2,$3,$4,'hash','nonce',$5,'active',now()+interval '1 hour')`, client+"-link", client, "local-"+client, "idempotency-"+client, subject); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES($1,$2,$3,$4,'active')`, client+"-link", client, "local-"+client, subject); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetExchangePolicy(ctx, "admin", "write", "research", "research-api", "research:read", true); err != nil {
		t.Fatal(err)
	}
	code, verifier, _ := authorizeCodeWithScopes(t, b, "openid profile email offline_access research:read")
	_, meBody := b.request("GET", "/api/v1/me", nil)
	var me struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(meBody, &me); err != nil || me.CSRF == "" {
		t.Fatal("missing session CSRF", err)
	}
	change := `{"caller_client":"write","target_client":"research","resource":"research-api","scope":"research:read","active":true}`
	postConsent := func(csrf, body string) int {
		req, _ := http.NewRequest("POST", b.issuer+"/api/v1/me/delegations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", b.issuer)
		req.Header.Set("X-CSRF-Token", csrf)
		resp, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if status := postConsent("invalid", change); status != 403 {
		t.Fatalf("missing CSRF accepted: %d", status)
	}
	if status := postConsent(me.CSRF, change); status != 204 {
		t.Fatalf("user consent failed: %d", status)
	}
	_, optionsBody := b.request("GET", "/api/v1/me/delegations", nil)
	var options []core.ExchangeOption
	if err := json.Unmarshal(optionsBody, &options); err != nil || len(options) != 1 || !options[0].Consented {
		t.Fatalf("user consent not listed: %s %v", optionsBody, err)
	}
	status, login := exchange(t, b, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {verifier}})
	if status != 200 {
		t.Fatalf("login %d %v", status, login)
	}
	source := login["access_token"].(string)
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "subject_token": {source}, "subject_token_type": {"urn:ietf:params:oauth:token-type:access_token"}, "requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"}, "resource": {"research-api"}, "audience": {"research-api"}, "scope": {"research:read"}}
	plainCode, plainVerifier, _ := authorizeCode(t, b)
	plainStatus, plainLogin := exchange(t, b, url.Values{"grant_type": {"authorization_code"}, "code": {plainCode}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {plainVerifier}})
	if plainStatus != 200 {
		t.Fatalf("plain login %d %v", plainStatus, plainLogin)
	}
	_, meBody = b.request("GET", "/api/v1/me", nil)
	if err := json.Unmarshal(meBody, &me); err != nil || me.CSRF == "" {
		t.Fatal("missing renewed session CSRF", err)
	}
	withoutScope := cloneValues(form)
	withoutScope.Set("subject_token", plainLogin["access_token"].(string))
	if status, _ := exchange(t, b, withoutScope); status == 200 {
		t.Fatal("source token scope was broadened by exchange")
	}
	status, result := exchange(t, b, form)
	if status != 200 {
		t.Fatalf("exchange %d %v", status, result)
	}
	if result["issued_token_type"] != string(oidc.AccessTokenType) || result["refresh_token"] != nil {
		t.Fatalf("wrong token type %v", result)
	}
	parsed, err := jwt.ParseSigned(result["access_token"].(string), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		ID        string            `json:"jti"`
		Subject   string            `json:"sub"`
		Audience  []string          `json:"aud"`
		Scope     string            `json:"scope"`
		Actor     map[string]string `json:"act"`
		Delegated bool              `json:"delegated"`
		Exp       int64             `json:"exp"`
	}
	if err := parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Subject != subject || len(claims.Audience) != 1 || claims.Audience[0] != "research-api" || claims.Scope != "research:read" || claims.Actor["sub"] != "write" || !claims.Delegated || time.Until(time.Unix(claims.Exp, 0)) > 6*time.Minute {
		t.Fatalf("unrestricted claims %+v", claims)
	}
	introspect := func(client, token string) map[string]any {
		req, _ := http.NewRequest("POST", b.issuer+"/oauth/introspect", strings.NewReader(url.Values{"token": {token}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(client, clientSecret)
		response, requestErr := b.http.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("introspection %d", response.StatusCode)
		}
		var value map[string]any
		if decodeErr := json.NewDecoder(response.Body).Decode(&value); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return value
	}
	if live := introspect("research", result["access_token"].(string)); live["active"] != true || live["sub"] != subject {
		t.Fatalf("target cannot introspect delegated token: %v", live)
	}
	sdk, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: "write", ClientSecret: clientSecret, RedirectURI: "http://127.0.0.1:3003/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	sdkToken, err := sdk.ExchangeToken(ctx, source, "research-api", []string{"research:read"})
	if err != nil || sdkToken.AccessToken == "" || sdkToken.IssuedTokenType != string(oidc.AccessTokenType) {
		t.Fatalf("Go SDK exchange: %+v %v", sdkToken, err)
	}
	bad := cloneValues(form)
	bad.Set("scope", "research:write")
	if status, _ = exchange(t, b, bad); status == 200 {
		t.Fatal("unpermitted scope exchanged")
	}
	bad = cloneValues(form)
	bad.Set("resource", "unknown-api")
	if status, _ = exchange(t, b, bad); status == 200 {
		t.Fatal("unregistered resource exchanged")
	}
	bad = cloneValues(form)
	bad.Set("requested_token_type", string(oidc.RefreshTokenType))
	if status, _ = exchange(t, b, bad); status == 200 {
		t.Fatal("refresh token issued by exchange")
	}
	if status := postConsent(me.CSRF, `{"caller_client":"write","target_client":"research","resource":"research-api","scope":"research:read","active":false}`); status != 204 {
		t.Fatalf("user consent revoke failed: %d", status)
	}
	if status, _ = exchange(t, b, form); status == 200 {
		t.Fatal("revoked consent exchanged")
	}
	if _, err := store.ActiveToken(ctx, claims.ID); err == nil {
		t.Fatal("consent revoke left delegated token active")
	}
	if status := postConsent(me.CSRF, change); status != 204 {
		t.Fatalf("regrant failed: %d", status)
	}
	status, result = exchange(t, b, form)
	if status != 200 {
		t.Fatalf("exchange after regrant: %d %v", status, result)
	}
	policyTokenID := jwtID(t, result["access_token"].(string), store)
	if err := store.SetExchangePolicy(ctx, "admin", "write", "research", "research-api", "research:read", false); err != nil {
		t.Fatal(err)
	}
	if status, _ = exchange(t, b, form); status == 200 {
		t.Fatal("disabled policy exchanged")
	}
	if _, err := store.ActiveToken(ctx, policyTokenID); err == nil {
		t.Fatal("policy disable left delegated token active")
	}
	if err := store.SetExchangePolicy(ctx, "admin", "write", "research", "research-api", "research:read", true); err != nil {
		t.Fatal(err)
	}
	status, result = exchange(t, b, form)
	if status != 200 {
		t.Fatalf("exchange after policy restore: %d %v", status, result)
	}
	targetTokenID := jwtID(t, result["access_token"].(string), store)
	targetLink, err := store.Link(ctx, "research", "research-link")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RevokeLink(ctx, "research", targetLink.ID, targetLink.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ActiveToken(ctx, targetTokenID); err == nil {
		t.Fatal("target link revoke left delegated token active")
	}
	if live := introspect("research", result["access_token"].(string)); live["active"] == true {
		t.Fatal("target introspection accepted revoked delegated token")
	}
	if status, _ = exchange(t, b, form); status == 200 {
		t.Fatal("exchange succeeded without target link")
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
	 VALUES('research-link-restored','research','local-research-restored','research-restored','hash','nonce',$1,'active',now()+interval '1 hour')`, subject); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES('research-link-restored','research','local-research-restored',$1,'active')`, subject); err != nil {
		t.Fatal(err)
	}
	status, result = exchange(t, b, form)
	if status != 200 {
		t.Fatalf("exchange after target relink: %d %v", status, result)
	}
	parentTokenID := jwtID(t, result["access_token"].(string), store)
	sourceParsed, err := jwt.ParseSigned(source, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var sourceClaims struct {
		ID string `json:"jti"`
	}
	if err = sourceParsed.Claims(&store.Keys.Private.PublicKey, &sourceClaims); err != nil {
		t.Fatal(err)
	}
	if problem := store.RevokeToken(ctx, sourceClaims.ID, subject, "write"); problem != nil {
		t.Fatal(problem)
	}
	if _, err = store.ActiveToken(ctx, parentTokenID); err == nil {
		t.Fatal("delegated token survived parent revocation")
	}
}

func jwtID(t *testing.T, raw string, store *core.Store) string {
	t.Helper()
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		ID string `json:"jti"`
	}
	if err = parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil || claims.ID == "" {
		t.Fatal("invalid token ID", err)
	}
	return claims.ID
}

func cloneValues(input url.Values) url.Values {
	copy := url.Values{}
	for key, values := range input {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}
