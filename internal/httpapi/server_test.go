package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

const clientSecret = "integration-client-secret-32-characters-long"

type browser struct {
	t      *testing.T
	http   *http.Client
	issuer string
}

func newBrowser(t *testing.T, issuer string) browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return browser{t: t, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, issuer: issuer}
}

func (b browser) request(method, path string, form url.Values) (*http.Response, []byte) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, b.issuer+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", b.issuer)
	}
	response, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return response, data
}

func (b browser) apiRequest(method, path string) (*http.Response, []byte) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.issuer+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Origin", b.issuer)
	for _, cookie := range b.http.Jar.Cookies(req.URL) {
		if cookie.Name == "fastcas_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	response, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return response, data
}
func csrf(t *testing.T, body []byte) string {
	t.Helper()
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing form CSRF")
	}
	return string(match[1])
}

func setup(t *testing.T) (*core.Store, browser) {
	t.Helper()
	store := testutil.Store(t)
	server := httptest.NewUnstartedServer(nil)
	issuer := "http://" + server.Listener.Addr().String()
	handler, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)
	err = store.RegisterClient(context.Background(), core.Client{ID: "write", Name: "FastWrite", Development: true, Redirects: []string{"http://127.0.0.1:3003/callback"}, Scopes: []string{"openid", "profile", "email", "offline_access"}}, clientSecret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateIdentity(context.Background(), "alice@example.test", "Alice", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	return store, newBrowser(t, issuer)
}
func authorizeCode(t *testing.T, b browser) (string, string, string) {
	return authorizeCodeWithScopes(t, b, "openid profile email offline_access")
}

func authorizeCodeWithScopes(t *testing.T, b browser, scopes string) (string, string, string) {
	return authorizeCodeFor(t, b, scopes, "alice@example.test", "correct horse battery staple")
}

func authorizeCodeFor(t *testing.T, b browser, scopes, email, password string) (string, string, string) {
	t.Helper()
	verifier, nonce := core.RandomToken(), core.RandomToken()
	sum := sha256.Sum256([]byte(verifier))
	params := url.Values{"client_id": {"write"}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "response_type": {"code"}, "scope": {scopes}, "state": {"browser-state"}, "nonce": {nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	callback := finishBrowserAuthorization(t, b, "/oauth/authorize?"+params.Encode(), email, password)
	destination, err := url.Parse(callback)
	if err != nil {
		t.Fatal(err)
	}
	if destination.Query().Get("state") != "browser-state" || destination.Query().Get("code") == "" {
		t.Fatal("invalid code response")
	}
	return destination.Query().Get("code"), verifier, nonce
}

// Follow both seamless SSO and the explicit consent paths used for binding.
func finishBrowserAuthorization(t *testing.T, b browser, authorize, email, password string) string {
	t.Helper()
	origin, _ := url.Parse(b.issuer)
	destination, _ := url.Parse(authorize)
	method, form := "GET", url.Values(nil)
	for range 10 {
		response, body := b.request(method, destination.RequestURI(), form)
		switch response.StatusCode {
		case 302, 303:
			next, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if next.IsAbs() && next.Host != origin.Host {
				return next.String()
			}
			destination = next
			method = "GET"
			form = nil
		case 200:
			id := destination.Query().Get("auth_request_id")
			form = url.Values{"csrf": {csrf(t, body)}, "auth_request_id": {id}}
			if strings.Contains(string(body), `name="password"`) {
				form.Set("action", "login")
				form.Set("email", email)
				form.Set("password", password)
			} else if strings.Contains(string(body), `value="approve"`) {
				form.Set("action", "approve")
			} else {
				t.Fatalf("unexpected authorization UI: %s", body)
			}
			destination, _ = url.Parse("/login")
			method = "POST"
		default:
			t.Fatalf("authorization failed: %d %s", response.StatusCode, body)
		}
	}
	t.Fatal("authorization redirect loop")
	return ""
}

func exchange(t *testing.T, b browser, values url.Values) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", b.issuer+"/oauth/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("write", clientSecret)
	resp, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func introspect(t *testing.T, b browser, token string) bool {
	t.Helper()
	req, err := http.NewRequest("POST", b.issuer+"/oauth/introspect", strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("write", clientSecret)
	response, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var data struct {
		Active bool `json:"active"`
	}
	if err = json.NewDecoder(response.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("introspection status %d", response.StatusCode)
	}
	return data.Active
}

func signedSID(t *testing.T, store *core.Store, raw string) string {
	t.Helper()
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		SID string `json:"sid"`
	}
	if err = parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.SID == "" {
		t.Fatal("ID token missing sid")
	}
	return claims.SID
}

func TestOIDCHTTPLoginRefreshAndReplay(t *testing.T) {
	store, b := setup(t)
	code, verifier, nonce := authorizeCode(t, b)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {verifier}}
	status, tokens := exchange(t, b, form)
	if status != 200 {
		t.Fatalf("token exchange status %d: %v", status, tokens)
	}
	parsed, err := jwt.ParseSigned(tokens["id_token"].(string), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Issuer   string   `json:"iss"`
		Audience []string `json:"aud"`
		Nonce    string   `json:"nonce"`
		Subject  string   `json:"sub"`
		SID      string   `json:"sid"`
		Name     string   `json:"name"`
		Email    string   `json:"email"`
	}
	if err = parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != b.issuer || claims.Nonce != nonce || claims.Subject == "" || len(claims.Audience) != 1 || claims.Audience[0] != "write" {
		t.Fatalf("invalid identity claims")
	}
	if claims.SID == "" {
		t.Fatal("authorization code ID token missing sid")
	}
	if claims.Name != "" || claims.Email != "" {
		t.Fatalf("authorization code ID token expanded profile claims: name=%q email=%q", claims.Name, claims.Email)
	}
	if status, _ = exchange(t, b, form); status == 200 {
		t.Fatal("authorization code replay accepted")
	}
	refresh := tokens["refresh_token"].(string)
	status, rotated := exchange(t, b, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if status != 200 {
		t.Fatalf("refresh status %d: %v", status, rotated)
	}
	if sid := signedSID(t, store, rotated["id_token"].(string)); sid != claims.SID {
		t.Fatalf("refresh ID token sid %q differs from authorization code sid %q", sid, claims.SID)
	}
	if status, _ = exchange(t, b, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}); status == 200 {
		t.Fatal("refresh replay accepted")
	}
	if status, _ = exchange(t, b, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rotated["refresh_token"].(string)}}); status == 200 {
		t.Fatal("replayed family still refreshes")
	}
}

func TestOIDCHTTPLogoutRevokesOnlyTheSelectedSession(t *testing.T) {
	localSessions := map[string]bool{}
	var receivedLogoutSID string
	var store *core.Store
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		logoutToken, err := url.ParseQuery(string(body))
		if err != nil || logoutToken.Get("logout_token") == "" {
			http.Error(w, "logout token required", http.StatusBadRequest)
			return
		}
		signed, err := jwt.ParseSigned(logoutToken.Get("logout_token"), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			http.Error(w, "invalid logout token", http.StatusBadRequest)
			return
		}
		var claims struct {
			SID string `json:"sid"`
		}
		if err = signed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil || claims.SID == "" {
			http.Error(w, "invalid logout sid", http.StatusBadRequest)
			return
		}
		receivedLogoutSID = claims.SID
		if _, ok := localSessions[claims.SID]; !ok {
			http.Error(w, "unknown local session", http.StatusBadRequest)
			return
		}
		localSessions[claims.SID] = false
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	var first browser
	store, first = setup(t)
	second := newBrowser(t, first.issuer)
	if _, err := store.DB.Exec(context.Background(), `UPDATE applications SET config=config || jsonb_build_object('backchannel_logout_uri',$1::text) WHERE id='write'`, receiver.URL); err != nil {
		t.Fatal(err)
	}
	var subject string
	if err := store.DB.QueryRow(context.Background(), `SELECT id FROM identities WHERE email='alice@example.test'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	linkID := core.RandomToken()
	if _, err := store.DB.Exec(context.Background(), `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at) VALUES($1,'write','local-first',$2,'logout-test-hash','logout-test-nonce',$3,'active',now()+interval '1 hour')`, linkID, linkID, subject); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(context.Background(), `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES($1,'write','local-first',$2,'active')`, linkID, subject); err != nil {
		t.Fatal(err)
	}
	firstCode, firstVerifier, _ := authorizeCode(t, first)
	status, firstTokens := exchange(t, first, url.Values{"grant_type": {"authorization_code"}, "code": {firstCode}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {firstVerifier}})
	if status != 200 {
		t.Fatalf("first token exchange status %d: %v", status, firstTokens)
	}
	firstSID := signedSID(t, store, firstTokens["id_token"].(string))

	secondCode, secondVerifier, _ := authorizeCode(t, second)
	status, secondTokens := exchange(t, second, url.Values{"grant_type": {"authorization_code"}, "code": {secondCode}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {secondVerifier}})
	if status != 200 {
		t.Fatalf("second token exchange status %d: %v", status, secondTokens)
	}
	secondSID := signedSID(t, store, secondTokens["id_token"].(string))
	if firstSID == secondSID {
		t.Fatalf("independent browser sessions share sid %q", firstSID)
	}
	localSessions[firstSID], localSessions[secondSID] = true, true
	firstAccess, firstRefresh := firstTokens["access_token"].(string), firstTokens["refresh_token"].(string)
	secondAccess, secondRefresh := secondTokens["access_token"].(string), secondTokens["refresh_token"].(string)
	if !introspect(t, first, firstAccess) || !introspect(t, second, secondAccess) {
		t.Fatal("both access tokens must be active before logout")
	}
	var firstLive, secondLive bool
	if err := store.DB.QueryRow(context.Background(), `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1`, firstSID).Scan(&firstLive); err != nil || !firstLive {
		t.Fatalf("first session missing before logout: %v", err)
	}
	if err := store.DB.QueryRow(context.Background(), `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1`, secondSID).Scan(&secondLive); err != nil || !secondLive {
		t.Fatalf("second session missing before logout: %v", err)
	}

	response, body := first.apiRequest("POST", "/api/v1/me/sessions/"+url.PathEscape(firstSID)+"/revoke")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("single-session logout status %d: %s", response.StatusCode, body)
	}
	if worked, err := store.DeliverEvent(context.Background(), first.issuer); err != nil || !worked {
		t.Fatalf("backchannel logout delivery failed: worked=%v err=%v", worked, err)
	}
	if err := store.DB.QueryRow(context.Background(), `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1`, firstSID).Scan(&firstLive); err != nil || firstLive {
		t.Fatal("selected session survived logout")
	}
	if err := store.DB.QueryRow(context.Background(), `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1`, secondSID).Scan(&secondLive); err != nil || !secondLive {
		t.Fatalf("unselected session was revoked: %v", err)
	}
	if introspect(t, first, firstAccess) {
		t.Fatal("selected access token survived logout")
	}
	if !introspect(t, second, secondAccess) {
		t.Fatal("unselected access token was revoked")
	}
	if status, _ := exchange(t, first, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {firstRefresh}}); status == http.StatusOK {
		t.Fatal("selected refresh token survived logout")
	}
	if status, _ := exchange(t, second, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {secondRefresh}}); status != http.StatusOK {
		t.Fatalf("unselected refresh token was revoked: %d", status)
	}
	if receivedLogoutSID != firstSID || localSessions[firstSID] || !localSessions[secondSID] {
		t.Fatalf("backchannel logout was not session-scoped: received=%q local=%v", receivedLogoutSID, localSessions)
	}
	if response, _ = first.request("GET", "/api/v1/me", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logged-out browser remained authenticated: %d", response.StatusCode)
	}
	if response, body = second.request("GET", "/api/v1/me", nil); response.StatusCode != http.StatusOK {
		t.Fatalf("unselected browser was logged out: %d %s", response.StatusCode, body)
	}
}
func TestWrongPKCEAndCrossOriginLoginRejected(t *testing.T) {
	store, b := setup(t)
	code, _, _ := authorizeCode(t, b)
	status, _ := exchange(t, b, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {core.RandomToken()}})
	if status == 200 {
		t.Fatal("wrong PKCE accepted")
	}
	loginPage, body := b.request("GET", "/login", nil)
	if loginPage.Header.Get("Referrer-Policy") != "same-origin" {
		t.Fatal("browser login must preserve a same-origin form Origin without leaking to other sites")
	}
	if policy := loginPage.Header.Get("Content-Security-Policy"); !strings.Contains(policy, "form-action 'self'") || strings.Contains(policy, "127.0.0.1:3003") {
		t.Fatalf("plain login must only submit locally: %s", policy)
	}
	authorization, err := store.CreateAuthRequest(context.Background(), &oidc.AuthRequest{
		ClientID: "write", RedirectURI: "http://127.0.0.1:3003/callback", ResponseType: oidc.ResponseTypeCode,
		Scopes: []string{"openid"}, State: core.RandomToken(), Nonce: core.RandomToken(),
		Prompt:        []string{oidc.PromptConsent},
		CodeChallenge: strings.Repeat("A", 43), CodeChallengeMethod: oidc.CodeChallengeMethodS256,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	authPage, _ := b.request("GET", "/login?auth_request_id="+url.QueryEscape(authorization.GetID()), nil)
	policy := authPage.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "form-action 'self' http://127.0.0.1:3003") || strings.Contains(policy, "attacker.example") {
		t.Fatalf("authorization form must allow only its registered callback origin: %s", policy)
	}
	req, _ := http.NewRequest("POST", b.issuer+"/login", strings.NewReader(url.Values{"csrf": {csrf(t, body)}, "email": {"alice@example.test"}, "password": {"correct horse battery staple"}, "action": {"login"}}.Encode()))
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross origin login accepted")
	}
}
func TestDiscoveryAndClientCredentials(t *testing.T) {
	store, b := setup(t)
	response, body := b.request("GET", "/.well-known/openid-configuration", nil)
	if response.StatusCode != 200 {
		t.Fatalf("discovery failed: %s", body)
	}
	var discovery map[string]any
	if err := json.Unmarshal(body, &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery["issuer"] != b.issuer || discovery["token_endpoint"] != b.issuer+"/oauth/token" {
		t.Fatal("discovery mismatch")
	}
	err := store.RegisterClient(context.Background(), core.Client{ID: "insight", Name: "FastInsight", Scopes: []string{"insight:publish"}, Resources: []string{"research-api"}, Grants: []oidc.GrantType{oidc.GrantTypeClientCredentials}}, clientSecret)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", b.issuer+"/oauth/token", strings.NewReader("grant_type=client_credentials&scope=insight%3Apublish"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("insight", clientSecret)
	response, err = b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var token map[string]any
	_ = json.NewDecoder(response.Body).Decode(&token)
	if response.StatusCode != 200 {
		t.Fatalf("service token status %d: %v", response.StatusCode, token)
	}
	parsed, err := jwt.ParseSigned(token["access_token"].(string), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Subject  string   `json:"sub"`
		Audience []string `json:"aud"`
		Service  bool     `json:"service"`
		Use      string   `json:"token_use"`
	}
	if err = parsed.Claims(&store.Keys.Private.PublicKey, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "insight" || !claims.Service || claims.Use != "access" || len(claims.Audience) != 1 || claims.Audience[0] != "research-api" {
		t.Fatal("machine token has incorrect identity boundary")
	}
}

func TestTypeScriptSDKAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_SDK_CONTRACT") != "1" {
		t.Skip("run with FASTCAS_SDK_CONTRACT=1 after building the TypeScript SDK")
	}
	store, b := setup(t)
	setupSDKExchange(t, store)
	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			if _, err := exec.LookPath(runtime); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, runtime, "../../sdk/typescript/test/contract.mjs")
			cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("SDK contract failed: %v\n%s", err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}

func TestPythonSDKAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_SDK_CONTRACT") != "1" {
		t.Skip("requires installed Python SDK in .venv")
	}
	store, b := setup(t)
	setupSDKExchange(t, store)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "../../.venv/bin/python", "../../sdk/python/tests/contract.py")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python SDK contract failed: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func setupSDKExchange(t *testing.T, store *core.Store) {
	t.Helper()
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
	for _, client := range []string{"research"} {
		if _, err := store.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
		 VALUES($1,$2,$3,$4,'hash','nonce',$5,'active',now()+interval '1 hour')`, client+"-sdk-link", client, "sdk-local-"+client, "sdk-idempotency-"+client, subject); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES($1,$2,$3,$4,'active')`, client+"-sdk-link", client, "sdk-local-"+client, subject); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetExchangePolicy(ctx, "admin", "write", "research", "research-api", "research:read", true); err != nil {
		t.Fatal(err)
	}
}

func TestDirectLoginReturnsToEnabledConsole(t *testing.T) {
	store, b := setup(t)
	_ = store
	// setup's handler is already constructed; the console flag is exercised
	// through a new handler that shares the same test store and issuer.
	server := httptest.NewUnstartedServer(nil)
	issuer := "http://" + server.Listener.Addr().String()
	handler, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	handler.ConsoleEnabled = true
	server.Config.Handler = handler
	server.Start()
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	b = browser{t, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, issuer}
	_, body := b.request("GET", "/login", nil)
	response, _ := b.request("POST", "/login", url.Values{"csrf": {csrf(t, body)}, "email": {"alice@example.test"}, "password": {"correct horse battery staple"}, "action": {"login"}})
	if response.StatusCode != 303 || response.Header.Get("Location") != "/console/" {
		t.Fatalf("direct login redirect: %d %s", response.StatusCode, response.Header.Get("Location"))
	}
}
