package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	fastcas "github.com/FastR-D/FastCAS/sdk/go"
)

func TestSSOAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1")
	}
	store := testutil.Store(t)
	cas := httptest.NewUnstartedServer(nil)
	issuer := "http://" + cas.Listener.Addr().String()
	api, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	cas.Config.Handler, err = httpapi.Console(api, filepath.Join(root, "web", "dist"))
	if err != nil {
		t.Fatal(err)
	}
	cas.Start()
	t.Cleanup(cas.Close)
	if _, err = store.CreateIdentity(context.Background(), "alice@example.test", "Alice", "correct horse battery staple", "member"); err != nil {
		t.Fatal(err)
	}
	makeRP := func(id string) string {
		rp := httptest.NewUnstartedServer(nil)
		origin := "http://" + rp.Listener.Addr().String()
		if err := store.RegisterClient(context.Background(), core.Client{ID: id, Name: id, Development: true, Redirects: []string{origin + "/callback"}, Scopes: []string{"openid", "profile", "email", "offline_access"}}, clientSecret); err != nil {
			t.Fatal(err)
		}
		client, err := fastcas.New(fastcas.Config{Issuer: issuer, ClientID: id, ClientSecret: clientSecret, RedirectURI: origin + "/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
		if err != nil {
			t.Fatal(err)
		}
		rp.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Path == "/start" {
				binding := fastcas.RandomBinding()
				http.SetCookie(w, &http.Cookie{Name: id + "-binding", Value: binding, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
				authorize, err := client.BeginLogin(r.Context(), fastcas.BeginOptions{BrowserBinding: binding, Scopes: []string{"openid", "profile", "email", "offline_access"}})
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				u, _ := url.Parse(authorize)
				if prompt := r.URL.Query().Get("prompt"); prompt != "" {
					q := u.Query()
					q.Set("prompt", prompt)
					u.RawQuery = q.Encode()
				}
				http.Redirect(w, r, u.String(), 303)
				return
			}
			if r.URL.Path == "/callback" {
				binding, err := r.Cookie(id + "-binding")
				if err != nil {
					http.Error(w, "missing binding", 400)
					return
				}
				// The SDK validates state and binding for error callbacks too.
				result, err := client.FinishLogin(r.Context(), origin+r.URL.RequestURI(), fastcas.FinishOptions{BrowserBinding: binding.Value})
				if err != nil {
					if r.URL.Query().Get("error") == "login_required" {
						fmt.Fprint(w, "<h1>login_required</h1>")
						return
					}
					http.Error(w, err.Error(), 400)
					return
				}
				if result.Identity.Email != "alice@example.test" {
					http.Error(w, "wrong identity", 400)
					return
				}
				fmt.Fprint(w, "<h1>项目工作台</h1>")
				return
			}
			http.NotFound(w, r)
		})
		rp.Start()
		t.Cleanup(rp.Close)
		return origin
	}
	first, second := makeRP("sso-first"), makeRP("sso-second")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "tests/sso_contract.mjs")
	cmd.Dir = filepath.Join("..", "..", "web")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ORIGIN="+issuer, "FASTCAS_FIRST_RP="+first, "FASTCAS_SECOND_RP="+second)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SSO browser contract failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
