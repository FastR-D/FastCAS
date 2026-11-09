package fastcas

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type pathTransactions struct{ values []Transaction }

func (store *pathTransactions) Put(_ context.Context, transaction Transaction) error {
	store.values = append(store.values, transaction)
	return nil
}

func (store *pathTransactions) Take(_ context.Context, _ string) (*Transaction, error) {
	return nil, nil
}

type pathTransport struct{ requests int }

func (transport *pathTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests++
	body := `{"id":"intent","nonce":"nonce"}`
	switch request.URL.Path {
	case "/.well-known/openid-configuration":
		body = `{"issuer":"https://auth.example","authorization_endpoint":"https://auth.example/authorize","token_endpoint":"https://auth.example/token","jwks_uri":"https://auth.example/jwks","userinfo_endpoint":"https://auth.example/userinfo","id_token_signing_alg_values_supported":["RS256"]}`
	case "/api/v1/link-intents":
	default:
		return nil, fmt.Errorf("unexpected request: %s", request.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func TestBeginReturnPaths(test *testing.T) {
	invalidPaths := []string{"https://attacker.test", "//attacker.test", "/\\attacker.test"}
	for character := rune(0); character <= 127; character++ {
		if character >= 32 && character != 127 {
			continue
		}
		invalidPaths = append(invalidPaths, "/"+string(character)+"/attacker.test", "/projects?query="+string(character), "/projects#"+string(character))
	}
	validPaths := []string{"", "/", "/projects?sort=recent&tag=a#details", "/项目/😀?q=中文#片段", "/encoded/%00%09%0A%0D%1F%7F%2F%5C%E4%B8%AD", "/unicode/\u0085\u2028"}
	for _, purpose := range []string{"login", "link", "register"} {
		test.Run(purpose, func(test *testing.T) {
			store := &pathTransactions{}
			transport := &pathTransport{}
			client, err := New(Config{Issuer: "https://auth.example", ClientID: "app", ClientSecret: "test-secret", RedirectURI: "https://app.example/callback", HTTPClient: &http.Client{Transport: transport}}, store)
			if err != nil {
				test.Fatal(err)
			}
			begin := func(path string) (string, error) {
				options := BeginOptions{BrowserBinding: strings.Repeat("b", 32), ReturnTo: path, LocalAccountRef: "local", LocalSessionID: "session"}
				switch purpose {
				case "link":
					return client.BeginLink(context.Background(), options)
				case "register":
					return client.BeginRegistration(context.Background(), "new-local", options)
				default:
					return client.BeginLogin(context.Background(), options)
				}
			}
			for _, path := range invalidPaths {
				test.Run(fmt.Sprintf("reject/%q", path), func(test *testing.T) {
					transactionsBefore, requestsBefore := len(store.values), transport.requests
					if _, err := begin(path); err == nil || err.Error() != "application-relative return path required" {
						test.Errorf("expected return path validation error for %q, got %v", path, err)
					}
					if len(store.values) != transactionsBefore || transport.requests != requestsBefore {
						test.Error("invalid return path reached HTTP or transaction storage")
					}
				})
			}
			for _, path := range validPaths {
				test.Run(fmt.Sprintf("accept/%q", path), func(test *testing.T) {
					authorization, err := begin(path)
					if err != nil {
						test.Fatal(err)
					}
					if !strings.HasPrefix(authorization, "https://auth.example/authorize?") {
						test.Fatalf("unexpected authorization URL: %s", authorization)
					}
					want := path
					if want == "" {
						want = "/"
					}
					if got := store.values[len(store.values)-1].ReturnTo; got != want {
						test.Errorf("return path = %q, want %q", got, want)
					}
				})
			}
		})
	}
}
