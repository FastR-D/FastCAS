package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	fastcas "github.com/FastR-D/FastCAS/sdk/go"
	oidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestRealJWKSOfflineRotationAndVerifierCache(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	dir := filepath.Join(t.TempDir(), "keys")
	if err := core.InitializeKeys(dir); err != nil {
		t.Fatal(err)
	}
	var mu sync.RWMutex
	var active http.Handler
	var offline bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		defer mu.RUnlock()
		if offline {
			http.Error(w, "maintenance", http.StatusServiceUnavailable)
			return
		}
		active.ServeHTTP(w, r)
	}))
	issuer := "http://" + server.Listener.Addr().String()
	load := func() {
		t.Helper()
		var err error
		store.Keys, err = core.LoadKeys(dir)
		if err != nil {
			t.Fatal(err)
		}
		active, err = httpapi.New(store, issuer, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	load()
	server.Start()
	defer server.Close()
	if err := store.RegisterClient(ctx, core.Client{ID: "write", Name: "test", Development: true, Redirects: []string{"http://127.0.0.1:3003/callback"}, Scopes: []string{"openid", "profile", "email", "offline_access"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateIdentity(ctx, "alice@example.test", "Alice", "correct horse battery staple", "member"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	b := browser{t, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, issuer}
	issue := func() (string, string) {
		t.Helper()
		code, verifier, _ := authorizeCode(t, b)
		status, tokens := exchange(t, b, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {"http://127.0.0.1:3003/callback"}})
		if status != 200 {
			t.Fatalf("exchange: %d", status)
		}
		return tokens["id_token"].(string), tokens["access_token"].(string)
	}
	newVerifier := func() *oidc.IDTokenVerifier {
		return oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(ctx, issuer+"/oauth/jwks"), &oidc.Config{ClientID: "write", SupportedSigningAlgs: []string{"RS256"}})
	}
	original, originalAccess := issue()
	sdk, err := fastcas.New(fastcas.Config{Issuer: issuer, ClientID: "write", ClientSecret: clientSecret, RedirectURI: "http://127.0.0.1:3003/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics, err := sdk.Diagnose(ctx); err != nil || !diagnostics.Ready || diagnostics.Issuer != issuer || diagnostics.ClientID != "write" {
		t.Fatalf("Go SDK diagnostics: %+v %v", diagnostics, err)
	}
	if _, err := sdk.VerifyAccessToken(ctx, originalAccess, "write"); err != nil {
		t.Fatal(err)
	}
	cached := newVerifier()
	if _, err := cached.Verify(ctx, original); err != nil {
		t.Fatal(err)
	}
	rotate := func(emergency bool) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if err := core.RotateKeys(dir, time.Hour, emergency); err != nil {
			t.Fatal(err)
		}
		load()
	}
	rotate(false)
	rotated, rotatedAccess := issue()
	if _, err := cached.Verify(ctx, rotated); err != nil {
		t.Fatal("unknown kid did not refresh", err)
	}
	if _, err := cached.Verify(ctx, original); err != nil {
		t.Fatal("retained old token rejected", err)
	}
	if _, err := sdk.VerifyAccessToken(ctx, rotatedAccess, "write"); err != nil {
		t.Fatal("Go SDK unknown kid did not refresh", err)
	}
	if _, err := sdk.VerifyAccessToken(ctx, originalAccess, "write"); err != nil {
		t.Fatal("Go SDK rejected retained old key", err)
	}
	resp, body := b.request("GET", "/oauth/jwks", nil)
	var keys jose.JSONWebKeySet
	if resp.StatusCode != 200 || json.Unmarshal(body, &keys) != nil || len(keys.Keys) != 2 {
		t.Fatal("JWKS missing retained key")
	}
	for _, key := range keys.Keys {
		if !key.IsPublic() {
			t.Fatal("JWKS leaked private material")
		}
	}
	rotate(true)
	// A populated verifier can still trust its cache: emergency operations must
	// explicitly replace it. This assertion prevents claiming immediate revocation.
	if _, err := cached.Verify(ctx, original); err != nil {
		t.Fatal("unexpected cache behavior", err)
	}
	if _, err := sdk.VerifyAccessToken(ctx, originalAccess, "write"); err != nil {
		t.Fatal("Go SDK cache behavior changed", err)
	}
	sdk.ResetVerificationCache()
	if _, err := sdk.VerifyAccessToken(ctx, originalAccess, "write"); err == nil {
		t.Fatal("Go SDK accepted removed key after reset")
	}
	fresh := newVerifier()
	if _, err := fresh.Verify(ctx, original); err == nil {
		t.Fatal("fresh verifier accepted removed key")
	}
	emergency, emergencyAccess := issue()
	if _, err := fresh.Verify(ctx, emergency); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.VerifyAccessToken(ctx, emergencyAccess, "write"); err != nil {
		t.Fatal("Go SDK rejected emergency key", err)
	}
	mu.Lock()
	offline = true
	mu.Unlock()
	if _, err := newVerifier().Verify(ctx, emergency); err == nil {
		t.Fatal("cold verifier accepted token during JWKS outage")
	}
	sdk.ResetVerificationCache()
	if _, err := sdk.VerifyAccessToken(ctx, emergencyAccess, "write"); err == nil {
		t.Fatal("Go SDK accepted token during JWKS outage after reset")
	}
}
