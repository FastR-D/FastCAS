package core_test

import (
	"context"
	"errors"
	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"testing"
	"time"
)

func TestClientSecretRotationGraceAndAudit(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	c := core.Client{ID: "service", Name: "Service", Redirects: []string{"http://127.0.0.1/callback"}, Development: true, Scopes: []string{"openid"}}
	old := "original-client-secret-32-characters-long"
	if err := s.RegisterClient(ctx, c, old); err != nil {
		t.Fatal(err)
	}
	next, err := s.RotateClientSecretAs(ctx, "administrator", c.ID, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next == old || len(next) < 32 {
		t.Fatal("rotation did not produce a new credential")
	}
	for _, secret := range []string{old, next} {
		if err = s.AuthorizeClientIDSecret(ctx, c.ID, secret); err != nil {
			t.Fatal("handover credential rejected", err)
		}
	}
	if _, err = s.DB.Exec(ctx, `UPDATE applications SET previous_secret_expires_at=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeClientIDSecret(ctx, c.ID, old); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("expired old secret accepted")
	}
	if err = s.AuthorizeClientIDSecret(ctx, c.ID, next); err != nil {
		t.Fatal("new secret expired with old one", err)
	}
	var audits int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='application.secret.rotate' AND target='service'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("rotation audit missing", err)
	}
	if _, err = s.RotateClientSecretAs(ctx, "administrator", c.ID, 2*time.Hour); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("unbounded grace accepted")
	}
	current, err := s.RotateClientSecretAs(ctx, "administrator", c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{old, next} {
		if err = s.AuthorizeClientIDSecret(ctx, c.ID, secret); !errors.Is(err, core.ErrUnauthorized) {
			t.Fatal("superseded secret accepted")
		}
	}
	if err = s.AuthorizeClientIDSecret(ctx, c.ID, current); err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterClient(ctx, core.Client{ID: "public", Name: "Public", Public: true, Redirects: []string{"http://127.0.0.1/callback"}, Development: true}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateClientSecretAs(ctx, "administrator", "public", time.Minute); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("public client rotation accepted")
	}
	// A failed audit rolls back both the new hash and the old-key grace window.
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_rotation_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='application.secret.rotate' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_rotation BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_rotation_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateClientSecretAs(ctx, "administrator", c.ID, time.Minute); err == nil {
		t.Fatal("rotation committed without audit")
	}
	if err = s.AuthorizeClientIDSecret(ctx, c.ID, current); err != nil {
		t.Fatal("audit failure changed credential", err)
	}
}
