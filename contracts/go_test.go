package contracts_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	fastcas "github.com/FastR-D/FastCAS/sdk/go"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixtureTransactions struct{}

func (fixtureTransactions) Put(context.Context, fastcas.Transaction) error { return nil }
func (fixtureTransactions) Take(context.Context, string) (*fastcas.Transaction, error) {
	return nil, nil
}

func TestSharedAccessTokenContracts(t *testing.T) {
	raw, err := os.ReadFile("access-tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Issuer string
		JWKS   json.RawMessage
		Cases  []struct {
			Name, Token, Audience string
			Scopes                []string
			Valid                 bool
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.String() {
		case fixture.Issuer + "/jwks":
			body = string(fixture.JWKS)
		case fixture.Issuer + "/.well-known/openid-configuration":
			b, _ := json.Marshal(map[string]any{"issuer": fixture.Issuer, "authorization_endpoint": fixture.Issuer + "/authorize", "token_endpoint": fixture.Issuer + "/token", "jwks_uri": fixture.Issuer + "/jwks", "userinfo_endpoint": fixture.Issuer + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}})
			body = string(b)
		default:
			return nil, fmt.Errorf("unexpected network request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	for _, v := range fixture.Cases {
		t.Run(v.Name, func(t *testing.T) {
			sdk, err := fastcas.New(fastcas.Config{Issuer: fixture.Issuer, ClientID: "fixture-client", RedirectURI: "https://rp.example.test/callback", HTTPClient: client}, fixtureTransactions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sdk.VerifyAccessToken(context.Background(), v.Token, v.Audience, v.Scopes...)
			if (err == nil) != v.Valid {
				t.Fatalf("valid=%v, error=%v", v.Valid, err)
			}
		})
	}
}
