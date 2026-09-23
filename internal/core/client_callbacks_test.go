package core_test

import (
	"context"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestUpdateClientCallbacksAtomic(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "app", Name: "App", Development: true,
		Redirects: []string{"http://127.0.0.1:8080/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateClientCallbacksAs(ctx, "admin", "app", "http://attacker.example/events", ""); err == nil {
		t.Fatal("external HTTP accepted")
	}
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION fail_callback_audit() RETURNS trigger AS $$ BEGIN IF NEW.action='application.callbacks.update' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql;
	 CREATE TRIGGER fail_callback_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_callback_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateClientCallbacksAs(ctx, "admin", "app", "http://127.0.0.1:8080/events", "http://127.0.0.1:8080/logout"); err == nil {
		t.Fatal("audit failure did not roll back")
	}
	got, err := s.Client(ctx, "app")
	if err != nil || got.EventsURL != "" || got.BackchannelURL != "" {
		t.Fatalf("partial callback update: %+v %v", got, err)
	}
	if _, err := s.DB.Exec(ctx, `DROP TRIGGER fail_callback_audit ON audit_events; DROP FUNCTION fail_callback_audit()`); err != nil {
		t.Fatal(err)
	}
	got, err = s.UpdateClientCallbacksAs(ctx, "admin", "app", "http://127.0.0.1:8080/events", "http://127.0.0.1:8080/logout")
	if err != nil || got.EventsURL != "http://127.0.0.1:8080/events" || got.BackchannelURL != "http://127.0.0.1:8080/logout" {
		t.Fatalf("callback update: %+v %v", got, err)
	}
}

func TestRegisterClientRejectsCSPDelimiterInRedirectHost(t *testing.T) {
	s := testutil.Store(t)
	for _, endpoint := range []string{"https://example.com;script-src/callback", "https://exa'mple.com/callback"} {
		if err := s.RegisterClient(context.Background(), core.Client{ID: "bad", Name: "Bad", Redirects: []string{endpoint}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err == nil {
			t.Fatalf("malformed redirect host accepted: %s", endpoint)
		}
	}
}

func TestUpdateClientCapabilitiesAtomic(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "app", Name: "App", Development: true, Redirects: []string{"http://127.0.0.1:8080/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	grants := []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeTokenExchange}
	if _, err := s.UpdateClientCapabilitiesAs(ctx, "admin", "app", grants, []string{"openid", "research:read"}, nil); err == nil {
		t.Fatal("exchange without resource accepted")
	}
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION fail_capability_audit() RETURNS trigger AS $$ BEGIN IF NEW.action='application.capabilities.update' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql;
	 CREATE TRIGGER fail_capability_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_capability_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateClientCapabilitiesAs(ctx, "admin", "app", grants, []string{"openid", "research:read"}, []string{"research-api"}); err == nil {
		t.Fatal("audit failure did not roll back")
	}
	client, err := s.Client(ctx, "app")
	if err != nil || len(client.Resources) != 0 {
		t.Fatalf("partial capability update %+v %v", client, err)
	}
	if _, err := s.DB.Exec(ctx, `DROP TRIGGER fail_capability_audit ON audit_events; DROP FUNCTION fail_capability_audit()`); err != nil {
		t.Fatal(err)
	}
	client, err = s.UpdateClientCapabilitiesAs(ctx, "admin", "app", grants, []string{"openid", "research:read"}, []string{"research-api"})
	if err != nil || len(client.Resources) != 1 || client.Resources[0] != "research-api" {
		t.Fatalf("capability update %+v %v", client, err)
	}
}
