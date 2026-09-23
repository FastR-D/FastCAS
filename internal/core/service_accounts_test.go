package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestServiceAccountLifecycleAndLeastPrivilege(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if _, err := s.CreateServiceAccountAs(ctx, "admin", "missing-resource", "Broken", []string{"insight:publish"}, nil, false); err == nil {
		t.Fatal("service without resource accepted")
	}
	if _, err := s.CreateServiceAccountAs(ctx, "admin", "openid-service", "Broken", []string{"openid"}, []string{"research-api"}, false); err == nil {
		t.Fatal("OIDC user scope accepted for service")
	}
	secret, err := s.CreateServiceAccountAs(ctx, "admin", "insight", "Insight", []string{"insight:publish"}, []string{"research-api"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeClientIDSecret(ctx, "insight", secret); err != nil {
		t.Fatal(err)
	}
	items, err := s.ServiceAccounts(ctx, "")
	if err != nil || len(items) != 1 || items[0].Client.ID != "insight" || !items[0].Active {
		t.Fatalf("service list: %+v %v", items, err)
	}
	if _, err = s.ClientCredentialsTokenRequest(ctx, "insight", []string{"reading:publish"}); err == nil {
		t.Fatal("unregistered scope accepted")
	}
	request, err := s.ClientCredentialsTokenRequest(ctx, "insight", []string{"insight:publish"})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := s.CreateAccessToken(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActiveToken(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateClientCapabilitiesAs(ctx, "admin", "insight", []oidc.GrantType{oidc.GrantTypeCode}, []string{"openid"}, nil); err == nil {
		t.Fatal("service converted into user client")
	}
	if _, err = s.UpdateClientCallbacksAs(ctx, "admin", "insight", "https://example.test/events", ""); err == nil {
		t.Fatal("service callback accepted")
	}
	rotated, err := s.RotateServiceAccountSecretAs(ctx, "admin", "insight", time.Minute)
	if err != nil || rotated == secret {
		t.Fatalf("service rotation: %v", err)
	}
	if err = s.AuthorizeClientIDSecret(ctx, "insight", secret); err != nil {
		t.Fatal("handover failed", err)
	}
	if _, err = s.SetServiceAccountActiveAs(ctx, "admin", "insight", false); err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeClientIDSecret(ctx, "insight", rotated); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("disabled secret accepted")
	}
	if _, err = s.ActiveToken(ctx, id); err == nil {
		t.Fatal("disabled service token active")
	}
	if _, err = s.ClientCredentialsTokenRequest(ctx, "insight", []string{"insight:publish"}); err == nil {
		t.Fatal("disabled service issued token")
	}
	if _, err = s.SetServiceAccountActiveAs(ctx, "admin", "insight", false); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.SetServiceAccountActiveAs(ctx, "admin", "insight", true)
	if err != nil || fresh == "" || fresh == rotated {
		t.Fatalf("service re-enable: %v", err)
	}
	if err = s.AuthorizeClientIDSecret(ctx, "insight", rotated); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old credential revived")
	}
	if err = s.AuthorizeClientIDSecret(ctx, "insight", fresh); err != nil {
		t.Fatal("new credential rejected", err)
	}
	if _, err = s.ActiveToken(ctx, id); err == nil {
		t.Fatal("old token revived")
	}
	items, err = s.ServiceAccounts(ctx, "")
	if err != nil || len(items) != 1 || !items[0].Active {
		t.Fatal("service not reactivated", err)
	}
	var audits int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target='insight' AND action IN ('service_account.disable','service_account.enable')`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("lifecycle audit: %d %v", audits, err)
	}
}
