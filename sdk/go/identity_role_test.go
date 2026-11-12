package fastcas

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type roleTransactions struct{ transaction *Transaction }

func (s *roleTransactions) Put(_ context.Context, tx Transaction) error {
	s.transaction = &tx
	return nil
}
func (s *roleTransactions) Take(_ context.Context, state string) (*Transaction, error) {
	tx := s.transaction
	s.transaction = nil
	if tx == nil || tx.State != state {
		return nil, nil
	}
	return tx, nil
}

type roleTransport func(*http.Request) (*http.Response, error)

func (f roleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifiedUserinfoSuppliesCurrentRole(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "role-test"))
	if err != nil {
		t.Fatal(err)
	}
	store := &roleTransactions{}
	role, nonce := "admin", ""
	transport := roleTransport(func(r *http.Request) (*http.Response, error) {
		var payload any
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			payload = map[string]any{"issuer": "https://cas.test", "authorization_endpoint": "https://cas.test/authorize", "token_endpoint": "https://cas.test/token", "userinfo_endpoint": "https://cas.test/userinfo", "jwks_uri": "https://cas.test/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}}
		case "/token":
			token, signErr := jwt.Signed(signer).Claims(map[string]any{"iss": "https://cas.test", "sub": "subject", "aud": "app", "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "nonce": nonce, "role": "admin"}).Serialize()
			if signErr != nil {
				return nil, signErr
			}
			payload = map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": token}
		case "/jwks":
			payload = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "role-test", Algorithm: "RS256", Use: "sig"}}}
		case "/userinfo":
			payload = map[string]any{"sub": "subject", "name": "User", "role": role}
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		raw, _ := json.Marshal(payload)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})
	client, err := New(Config{Issuer: "https://cas.test", ClientID: "app", ClientSecret: "secret", RedirectURI: "https://app.test/callback", HTTPClient: &http.Client{Transport: transport}}, store)
	if err != nil {
		t.Fatal(err)
	}
	binding := strings.Repeat("b", 32)
	for _, role = range []string{"admin", "member", ""} {
		authorization, err := client.BeginLogin(context.Background(), BeginOptions{BrowserBinding: binding})
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := url.Parse(authorization)
		nonce = parsed.Query().Get("nonce")
		result, err := client.FinishLogin(context.Background(), "https://app.test/callback?code=code&state="+parsed.Query().Get("state"), FinishOptions{BrowserBinding: binding})
		if err != nil {
			t.Fatal(err)
		}
		if result.Identity.Role != role {
			t.Fatalf("role = %q, want verified userinfo role %q", result.Identity.Role, role)
		}
	}
}
