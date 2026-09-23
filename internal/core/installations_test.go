package core_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestRevokedInstallationUpgradeRevokesExistingDeviceToken(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "device-upgrade", Name: "Device", Public: true,
		Scopes: []string{"openid"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateIdentity(ctx, "upgrade@example.test", "Upgrade", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	token := core.Token{ID: "old-device-token", ClientID: "device-upgrade", Subject: user.ID,
		Audience: []string{"device-upgrade"}, Scopes: []string{"openid"}, Grant: "device_code", ExpiresAt: time.Now().Add(5 * time.Minute)}
	payload, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO access_tokens(id,client_id,subject,payload,expires_at) VALUES($1,$2,$3,$4,$5)`,
		token.ID, token.ClientID, token.Subject, payload, token.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash,revoked_at)
	 VALUES('old-install','device-upgrade',$1,'550e8400-e29b-41d4-a716-446655440000',$2,$3,now())`,
		user.ID, token.ID, core.Hash("old-install-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActiveToken(ctx, token.ID); err != nil {
		t.Fatalf("fixture token was not active before upgrade: %v", err)
	}
	if _, err = s.DB.Exec(ctx, `DELETE FROM schema_migrations WHERE version='012_revoke_device_installation_tokens.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActiveToken(ctx, token.ID); err != core.ErrUnauthorized {
		t.Fatalf("upgrade left revoked installation token active: %v", err)
	}
}
