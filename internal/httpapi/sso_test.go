package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func ssoRequest(verifier, prompt string) url.Values {
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {"write"}, "redirect_uri": {"http://127.0.0.1:3003/callback"}, "response_type": {"code"}, "scope": {"openid profile email offline_access"}, "state": {"seamless-state"}, "nonce": {core.RandomToken()}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	if prompt != "" {
		q.Set("prompt", prompt)
	}
	return q
}

func TestSSOReuseAndSilentRequestsDoNotDisplayLoginOrConsent(t *testing.T) {
	for _, prompt := range []string{"", "none"} {
		t.Run("prompt="+prompt, func(t *testing.T) {
			store, b := setup(t)
			ctx := context.Background()
			u, err := store.Authenticate(ctx, "alice@example.test", "correct horse battery staple", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := store.NewSession(ctx, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			origin, _ := url.Parse(b.issuer)
			b.http.Jar.SetCookies(origin, []*http.Cookie{{Name: "fastcas_session", Value: token, Path: "/"}})
			verifier := core.RandomToken()
			q := ssoRequest(verifier, prompt)
			response, _ := b.request("GET", "/oauth/authorize?"+q.Encode(), nil)
			if response.StatusCode != 302 || !strings.HasPrefix(response.Header.Get("Location"), "/authorize/continue?") {
				t.Fatal("did not use seamless continuation", response.StatusCode, response.Header.Get("Location"))
			}
			response, body := b.request("GET", response.Header.Get("Location"), nil)
			if response.StatusCode != 303 || strings.Contains(string(body), "<form") {
				t.Fatalf("SSO showed an interaction: %d %s", response.StatusCode, body)
			}
			if strings.Contains(response.Header.Get("Location"), "/login") {
				t.Fatal("SSO returned to login")
			}
			callback, _ := url.Parse(response.Header.Get("Location"))
			response, body = b.request("GET", callback.RequestURI(), nil)
			destination, _ := url.Parse(response.Header.Get("Location"))
			if response.StatusCode != 302 || destination.Query().Get("state") != "seamless-state" || destination.Query().Get("code") == "" {
				t.Fatalf("bad callback: %d %s", response.StatusCode, body)
			}
			values := url.Values{"grant_type": {"authorization_code"}, "code": {destination.Query().Get("code")}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {verifier}}
			if status, result := exchange(t, b, values); status != 200 || result["id_token"] == nil {
				t.Fatalf("silent code did not exchange: %d %v", status, result)
			}
			if status, _ := exchange(t, b, values); status != 400 {
				t.Fatal("silent authorization code replay accepted", status)
			}
		})
	}
}

func TestSSOInteractionBoundariesAndNoUIErrors(t *testing.T) {
	cases := []struct{ name, prompt, state, want string }{
		{"anonymous", "none", "anonymous", "login_required"},
		{"revoked", "none", "revoked", "login_required"},
		{"expired", "none", "expired", "login_required"},
		{"disabled", "none", "disabled", "login_required"},
		{"stale max_age", "none", "stale", "login_required"},
		{"subject mismatch", "none", "mismatch", "login_required"},
		{"binding cannot be silent", "none", "binding", "interaction_required"},
		{"resource scope cannot be silent", "none", "resource", "interaction_required"},
		{"explicit consent", "consent", "valid", "consent"},
		{"explicit login", "login", "valid", "password"},
		{"max_age zero", "", "zero", "password"},
		{"binding confirmation", "", "binding", "consent"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			store, b := setup(t)
			ctx := context.Background()
			origin, _ := url.Parse(b.issuer)
			user, err := store.Authenticate(ctx, "alice@example.test", "correct horse battery staple", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := store.NewSession(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			session, err := store.Session(ctx, token)
			if err != nil {
				t.Fatal(err)
			}
			if item.state != "anonymous" {
				b.http.Jar.SetCookies(origin, []*http.Cookie{{Name: "fastcas_session", Value: token, Path: "/"}})
			}
			q := ssoRequest(core.RandomToken(), item.prompt)
			switch item.state {
			case "revoked":
				_, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET revoked_at=now() WHERE id=$1`, session.ID)
			case "expired":
				_, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session.ID)
			case "disabled":
				_, err = store.DB.Exec(ctx, `UPDATE identities SET status='disabled' WHERE id=$1`, user.ID)
			case "stale":
				q.Set("max_age", "1")
				_, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '10 minutes' WHERE id=$1`, session.ID)
			case "zero":
				q.Set("max_age", "0")
			case "binding":
				var intent *core.LinkIntent
				intent, err = store.CreateLinkIntent(ctx, "write", "original-local-account", core.RandomToken())
				if err == nil {
					q.Set("nonce", intent.Nonce)
				}
			case "resource":
				_, err = store.UpdateClientCapabilitiesAs(ctx, "fixture", "write", []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}, []string{"openid", "profile", "email", "offline_access", "research:read"}, nil)
				q.Set("scope", "openid research:read")
			}
			if err != nil {
				t.Fatal(err)
			}
			response, _ := b.request("GET", "/oauth/authorize?"+q.Encode(), nil)
			next, _ := url.Parse(response.Header.Get("Location"))
			id := next.Query().Get("auth_request_id")
			if id == "" {
				t.Fatalf("no authorization transaction: %d %s", response.StatusCode, next)
			}
			if item.state == "mismatch" {
				if _, err = store.DB.Exec(ctx, `UPDATE auth_requests SET payload=jsonb_set(payload,'{RequestedSubject}',to_jsonb('another-subject'::text)) WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			response, body := b.request("GET", next.RequestURI(), nil)
			if item.want == "password" || item.want == "consent" {
				if response.StatusCode != 200 || !strings.Contains(string(body), `name="csrf"`) {
					t.Fatalf("required interaction missing: %d %s", response.StatusCode, body)
				}
				if item.want == "password" && !strings.Contains(string(body), `name="password"`) {
					t.Fatal("fresh primary proof bypassed")
				}
				if item.want == "consent" && !strings.Contains(string(body), `value="approve"`) {
					t.Fatal("explicit consent bypassed")
				}
				return
			}
			callback, _ := url.Parse(response.Header.Get("Location"))
			if response.StatusCode != 302 || callback.Query().Get("error") != item.want || callback.Query().Get("state") != "seamless-state" {
				t.Fatalf("bad silent error: %d %s %s", response.StatusCode, callback, body)
			}
			if strings.Contains(string(body), "<form") {
				t.Fatal("prompt=none displayed login UI")
			}
		})
	}
}

func TestFreshPasswordLoginReturnsToClientWithoutApproval(t *testing.T) {
	for _, prompt := range []string{"", "login"} {
		t.Run(prompt, func(t *testing.T) {
			_, b := setup(t)
			q := ssoRequest(core.RandomToken(), prompt)
			// max_age=0 and prompt=login must become satisfiable after a new login.
			if prompt == "login" {
				q.Set("max_age", "0")
			}
			target := finishBrowserAuthorization(t, b, "/oauth/authorize?"+q.Encode(), "alice@example.test", "correct horse battery staple")
			u, _ := url.Parse(target)
			if u.Query().Get("code") == "" {
				t.Fatal("fresh login did not finish", target)
			}
		})
	}
}

func TestSSORemembersResourceApprovalButRequiresNewScopes(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	if _, err := store.UpdateClientCapabilitiesAs(ctx, "fixture", "write", []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}, []string{"openid", "profile", "email", "offline_access", "research:read", "research:write"}, nil); err != nil {
		t.Fatal(err)
	}
	q := ssoRequest(core.RandomToken(), "")
	q.Set("scope", "openid research:read")
	target := finishBrowserAuthorization(t, b, "/oauth/authorize?"+q.Encode(), "alice@example.test", "correct horse battery staple")
	callback, _ := url.Parse(target)
	if callback.Query().Get("code") == "" {
		t.Fatal("initial approval failed")
	}
	for _, item := range []struct{ scopes, want string }{{"openid research:read", ""}, {"openid research:read research:write", "interaction_required"}} {
		q = ssoRequest(core.RandomToken(), "none")
		q.Set("scope", item.scopes)
		response, _ := b.request("GET", "/oauth/authorize?"+q.Encode(), nil)
		next, _ := url.Parse(response.Header.Get("Location"))
		response, body := b.request("GET", next.RequestURI(), nil)
		if strings.Contains(string(body), "<form") {
			t.Fatal("silent resource request showed UI")
		}
		destination, _ := url.Parse(response.Header.Get("Location"))
		if item.want == "" {
			if response.StatusCode != 303 {
				t.Fatal("approved scopes were not reused", response.StatusCode)
			}
			response, _ = b.request("GET", destination.RequestURI(), nil)
			destination, _ = url.Parse(response.Header.Get("Location"))
			if destination.Query().Get("code") == "" {
				t.Fatal("approved scopes did not issue code")
			}
		} else if destination.Query().Get("error") != item.want {
			t.Fatal("new scopes bypassed confirmation", response.StatusCode)
		}
	}
}

func TestSSOCompletionRejectsSessionRevokedAfterLookup(t *testing.T) {
	store, _ := setup(t)
	ctx := context.Background()
	user, err := store.Authenticate(ctx, "alice@example.test", "correct horse battery staple", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateAuthRequest(ctx, &oidc.AuthRequest{ClientID: "write", RedirectURI: "http://127.0.0.1:3003/callback", ResponseType: oidc.ResponseTypeCode, Scopes: []string{"openid"}, State: core.RandomToken(), Nonce: core.RandomToken(), CodeChallenge: strings.Repeat("A", 43), CodeChallengeMethod: oidc.CodeChallengeMethodS256}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.LogoutSession(ctx, user.ID, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteAuthorization(ctx, request.GetID(), snapshot); err == nil {
		t.Fatal("revoked session snapshot authorized a login")
	}
}
