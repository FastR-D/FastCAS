package httpapi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	fastcas "github.com/FastR-D/FastCAS/sdk/go"
)

// In-memory state is restricted to tests. Applications supply persistent stores.
type transactions struct {
	mu    sync.Mutex
	items map[string]fastcas.Transaction
}

func (s *transactions) Put(_ context.Context, t fastcas.Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[t.State] = t
	return nil
}
func (s *transactions) Take(_ context.Context, state string) (*fastcas.Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[state]
	delete(s.items, state)
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func completeSDKAuthorization(t *testing.T, b browser, authorize string) string {
	t.Helper()
	u, _ := url.Parse(authorize)
	response, body := b.request("GET", u.RequestURI(), nil)
	if response.StatusCode != 302 {
		t.Fatalf("authorize %d", response.StatusCode)
	}
	login, _ := url.Parse(response.Header.Get("Location"))
	requestID := login.Query().Get("auth_request_id")
	_, body = b.request("GET", login.RequestURI(), nil)
	if strings.Contains(string(body), `name="password"`) {
		response, body = b.request("POST", "/login", url.Values{"csrf": {csrf(t, body)}, "auth_request_id": {requestID}, "email": {"alice@example.test"}, "password": {"correct horse battery staple"}, "action": {"login"}})
		if response.StatusCode != 303 {
			t.Fatalf("login %d", response.StatusCode)
		}
		_, body = b.request("GET", response.Header.Get("Location"), nil)
	}
	response, body = b.request("POST", "/login", url.Values{"csrf": {csrf(t, body)}, "auth_request_id": {requestID}, "action": {"approve"}})
	if response.StatusCode != 303 {
		t.Fatalf("consent %d", response.StatusCode)
	}
	callback, _ := url.Parse(response.Header.Get("Location"))
	response, _ = b.request("GET", callback.RequestURI(), nil)
	if response.StatusCode != 302 {
		t.Fatalf("callback %d", response.StatusCode)
	}
	return response.Header.Get("Location")
}
func TestGoSDKLoginAndLinkContract(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	client, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: "write", ClientSecret: clientSecret, RedirectURI: "http://127.0.0.1:3003/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	binding := fastcas.RandomBinding()
	auth, err := client.BeginLogin(ctx, fastcas.BeginOptions{BrowserBinding: binding, Scopes: []string{"openid", "profile", "email", "offline_access"}})
	if err != nil {
		t.Fatal(err)
	}
	callback := completeSDKAuthorization(t, b, auth)
	result, err := client.FinishLogin(ctx, callback, fastcas.FinishOptions{BrowserBinding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity.Email != "alice@example.test" || result.Identity.Name != "Alice" {
		t.Fatal("profile mismatch")
	}
	if _, err = client.FinishLogin(ctx, callback, fastcas.FinishOptions{BrowserBinding: binding}); err == nil {
		t.Fatal("callback replay accepted")
	}
	if _, err = client.VerifyAccessToken(ctx, result.Token.AccessToken, "write"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.VerifyAccessToken(ctx, result.IDToken, "write"); err == nil {
		t.Fatal("ID token accepted at resource server")
	}
	if _, err = client.ResolveLink(ctx, result.Identity.Subject); err == nil {
		t.Fatal("unlinked identity resolved")
	}
	auth, err = client.BeginLink(ctx, fastcas.BeginOptions{BrowserBinding: binding, LocalAccountRef: "go-original-user", LocalSessionID: "go-session"})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := client.FinishLogin(ctx, completeSDKAuthorization(t, b, auth), fastcas.FinishOptions{BrowserBinding: binding, LocalAccountRef: "go-original-user", LocalSessionID: "go-session"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := client.PrepareLink(ctx, linked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.ResolveLink(ctx, result.Identity.Subject); err == nil {
		t.Fatal("prepared link used for login")
	}
	active, err := client.ActivateLink(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ResolveLink(ctx, result.Identity.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.LocalRef != "go-original-user" {
		t.Fatal("local account changed")
	}
	revoked, err := client.RevokeLink(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != "revoked" {
		t.Fatal("not revoked")
	}
	if _, err = client.ActivateLink(ctx, active.ID); err == nil {
		t.Fatal("revoked binding revived")
	}
	var received string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		received = string(raw)
		if err := client.HandleEvent(r.Context(), received, func(_ context.Context, event fastcas.AccountLinkEvent) error {
			if event.Link.ID != revoked.ID || event.Link.Version != revoked.Version || event.Link.LocalRef != "go-original-user" {
				return errors.New("event did not match revoked binding")
			}
			return nil
		}); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	if _, err := store.DB.Exec(ctx, `UPDATE applications SET config=jsonb_set(config,'{events_uri}',to_jsonb($1::text)) WHERE id='write'`, receiver.URL); err != nil {
		t.Fatal(err)
	}
	if worked, err := store.DeliverEvent(ctx, b.issuer); err != nil || !worked {
		t.Fatalf("event not delivered: %v", err)
	}
	if received == "" {
		t.Fatal("event missing")
	}
	if err := client.HandleEvent(ctx, result.IDToken, func(context.Context, fastcas.AccountLinkEvent) error {
		t.Error("ID token accepted as event")
		return nil
	}); err == nil {
		t.Fatal("ID token accepted")
	}
	if err := client.HandleEvent(ctx, received, func(context.Context, fastcas.AccountLinkEvent) error { return errors.New("local transaction failed") }); err == nil {
		t.Fatal("callback failure swallowed")
	}
	registrationURL, err := client.BeginRegistration(ctx, "go-new-user", fastcas.BeginOptions{BrowserBinding: binding})
	if err != nil {
		t.Fatal(err)
	}
	registration, err := client.FinishLogin(ctx, completeSDKAuthorization(t, b, registrationURL), fastcas.FinishOptions{BrowserBinding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if registration.Transaction.Purpose != "register" {
		t.Fatal("registration lost its purpose")
	}
	newLink, err := client.PrepareLink(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	if newLink.LocalRef != "go-new-user" {
		t.Fatal("registration account reference changed")
	}
}

func TestGoSDKIdentityStatusNotification(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	client, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: "write", ClientSecret: clientSecret, RedirectURI: "http://127.0.0.1:3003/callback", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	var notice fastcas.Notification
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := client.HandleNotification(r.Context(), string(body), func(_ context.Context, event fastcas.Notification) error { notice = event; return nil }); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	var subject string
	if err = store.DB.QueryRow(ctx, `SELECT id FROM identities WHERE email='alice@example.test'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at) VALUES('sdk-status','write','local-user','sdk-status-idempotency','hash','nonce',$1,'active',now()+interval '1 hour')`, subject); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES('sdk-status','write','local-user',$1,'active')`, subject); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE applications SET config=jsonb_set(config,'{events_uri}',to_jsonb($1::text)) WHERE id='write'`, receiver.URL); err != nil {
		t.Fatal(err)
	}
	if err = store.SetIdentityStatus(ctx, "admin", subject, "disabled"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if worked, err := store.DeliverEvent(ctx, b.issuer); err != nil || !worked {
			t.Fatalf("status delivery: %v %v", worked, err)
		}
	}
	if notice.Type != "identity.status_changed" || notice.Subject != subject || notice.Status != "disabled" || notice.Version != 2 {
		t.Fatalf("invalid SDK notice: %+v", notice)
	}
}
