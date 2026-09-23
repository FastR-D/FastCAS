package core_test

import (
	"context"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestFenceRestoredStateRevokesSnapshotCredentialsAndBindings(t *testing.T) {
	s, identity, session := fixture(t)
	ctx := context.Background()
	a := authorize(t, s, session, core.RandomToken())
	access, refresh, _, err := s.CreateAccessAndRefreshTokens(ctx, a, "")
	if err != nil {
		t.Fatal(err)
	}
	installationSecret := core.RandomToken()
	if _, err = s.DB.Exec(ctx, `INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash)
	 VALUES('restored-installation','write',$1,'550e8400-e29b-41d4-a716-446655440000',$2,$3)`, identity.ID, access, core.Hash(installationSecret)); err != nil {
		t.Fatal(err)
	}
	intent, err := s.CreateLinkIntent(ctx, "write", "local-recovered-user", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	authorize(t, s, session, intent.Nonce)
	link, err := s.PrepareLink(ctx, "write", intent.ID, intent.Nonce, identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	link, err = s.ActivateLink(ctx, "write", link.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FenceRestoredState(ctx); err != nil {
		t.Fatal(err)
	}
	var live bool
	if err = s.DB.QueryRow(ctx, `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1`, session.ID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live {
		t.Fatal("restored browser session survived")
	}
	if _, err = s.ActiveToken(ctx, access); err == nil {
		t.Fatal("restored access token survived")
	}
	installation, err := s.DeviceInstallationBySecret(ctx, "restored-installation", installationSecret)
	if err != nil || installation.RevokedAt == nil {
		t.Fatalf("restored installation survived: %+v %v", installation, err)
	}
	if _, err = s.TokenRequestByRefreshToken(ctx, refresh); err == nil {
		t.Fatal("restored refresh token survived")
	}
	if _, err = s.ResolveLink(ctx, "write", identity.ID); err == nil {
		t.Fatal("restored link survived")
	}
	var state string
	var version int64
	if err = s.DB.QueryRow(ctx, `SELECT state,version FROM account_links WHERE id=$1`, link.ID).Scan(&state, &version); err != nil {
		t.Fatal(err)
	}
	if state != "revoked" || version != link.Version+1 {
		t.Fatalf("link fence: %s v%d", state, version)
	}
	var notifications int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE client_id='write' AND delivered_at IS NULL AND dead_at IS NULL`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications < 2 {
		t.Fatalf("missing logout/revocation notifications: %d", notifications)
	}
	if _, err = s.Authenticate(ctx, identity.Email, "correct horse battery staple", "recovery-test"); err != nil {
		t.Fatalf("CAS identity disabled by fence: %v", err)
	}
	if err = s.FenceRestoredState(ctx); err != nil {
		t.Fatalf("repeat fence: %v", err)
	}
	installation, err = s.DeviceInstallationBySecret(ctx, "restored-installation", installationSecret)
	if err != nil || installation.RevokedAt == nil {
		t.Fatalf("repeat fence revived installation: %+v %v", installation, err)
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE client_id='write' AND delivered_at IS NULL AND dead_at IS NULL`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications < 2 {
		t.Fatalf("repeat fence lost notifications: %d", notifications)
	}
}
