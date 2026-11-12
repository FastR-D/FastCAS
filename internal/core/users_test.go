package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestUpdateUserProfileAndRevokeOldCredentials(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, token := linkedLogoutFixture(t, s, "http://127.0.0.1:19001")
	credential, err := s.IssueCredential(ctx, "operator", "recovery", u.Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE identities SET email_verified=true WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateIdentity(ctx, "operator", u.ID, " UPDATED@example.test ", " Updated User ", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "updated@example.test" || updated.Name != "Updated User" || updated.Role != "admin" || updated.EmailVerified || updated.CreatedAt.IsZero() {
		t.Fatalf("incorrect updated profile: %+v", updated)
	}
	if _, err = s.Session(ctx, token); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("profile change retained prior session")
	}
	if _, err = s.RedeemCredential(ctx, "recovery", credential, "", "new recovery password long enough"); !errors.Is(err, core.ErrExpired) {
		t.Fatal("profile change retained old recovery credential")
	}
	if _, err = s.UpdateIdentity(ctx, u.ID, u.ID, updated.Email, updated.Name, "member"); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("self role change accepted")
	}
	if _, err = s.UpdateIdentity(ctx, "operator", u.ID, "invalid", "Name", "admin"); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("invalid email accepted")
	}
	other, err := s.CreateIdentity(ctx, "other@example.test", "Other", "another long password for users", "member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateIdentity(ctx, "operator", u.ID, other.Email, "Must roll back", "admin"); !errors.Is(err, core.ErrConflict) {
		t.Fatal("duplicate email accepted")
	}
	current, err := s.Identity(ctx, u.ID)
	if err != nil || current.Name != "Updated User" || current.Email != updated.Email {
		t.Fatal("failed edit changed existing profile")
	}
}

func TestDeleteUserRevokesAccessAndPreservesAuditAtomically(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, token := linkedLogoutFixture(t, s, "http://127.0.0.1:19002")
	credential, err := s.IssueCredential(ctx, "operator", "recovery", u.Email)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO token_families(id,subject,client_id,expires_at) VALUES('user-family',$1,'receiver',now()+interval '1 hour')`,
		`INSERT INTO access_tokens(id,subject,client_id,payload,expires_at) VALUES('user-access',$1,'receiver','{}',now()+interval '1 hour')`,
		`INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash) VALUES('user-installation','receiver',$1,gen_random_uuid(),'user-access','secret-hash')`,
		`INSERT INTO exchange_consents(subject,caller_client,target_client,resource,scope) VALUES($1,'receiver','receiver','resource','scope')`,
		`INSERT INTO recovery_codes(identity_id,token_hash) VALUES($1,'recovery-hash')`,
		`UPDATE identities SET mfa_secret='\x01'::bytea WHERE id=$1`,
	} {
		if _, err = s.DB.Exec(ctx, statement, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_user_delete_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='identity.delete' THEN RAISE EXCEPTION 'injected user delete failure'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER reject_user_delete_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_user_delete_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteIdentity(ctx, "operator", u.ID); err == nil {
		t.Fatal("audit failure did not abort deletion")
	}
	if _, err = s.Session(ctx, token); err != nil {
		t.Fatal("failed delete revoked session")
	}
	var state string
	if err = s.DB.QueryRow(ctx, `SELECT state FROM account_links WHERE subject=$1`, u.ID).Scan(&state); err != nil || state != "active" {
		t.Fatal("failed deletion changed binding")
	}
	if _, err = s.DB.Exec(ctx, `DROP TRIGGER reject_user_delete_audit ON audit_events`); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteIdentity(ctx, "operator", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("deleted user retained session")
	}
	if _, err = s.Authenticate(ctx, u.Email, "correct horse battery staple", "delete-test"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("deleted user can authenticate")
	}
	if err = s.SetIdentityStatus(ctx, "operator", u.ID, "active"); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("deleted user could be re-enabled")
	}
	if _, err = s.UpdateIdentity(ctx, "operator", u.ID, u.Email, "Restored", "member"); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("deleted user could be edited")
	}
	if _, err = s.RedeemCredential(ctx, "recovery", credential, "", "new recovery password long enough"); !errors.Is(err, core.ErrExpired) {
		t.Fatal("deleted user retained recovery credential")
	}
	checks := []string{
		`SELECT deleted_at IS NOT NULL AND status='disabled' AND email<>$2 AND password_hash='' AND mfa_secret IS NULL FROM identities WHERE id=$1`,
		`SELECT revoked_at IS NOT NULL FROM token_families WHERE subject=$1 AND $2<>''`,
		`SELECT revoked_at IS NOT NULL FROM access_tokens WHERE subject=$1 AND $2<>''`,
		`SELECT revoked_at IS NOT NULL FROM device_installations WHERE subject=$1 AND $2<>''`,
		`SELECT revoked_at IS NOT NULL FROM exchange_consents WHERE subject=$1 AND $2<>''`,
		`SELECT state='revoked' AND version=2 FROM account_links WHERE subject=$1 AND $2<>''`,
		`SELECT NOT EXISTS(SELECT 1 FROM recovery_codes WHERE identity_id=$1) AND $2<>''`,
		`SELECT EXISTS(SELECT 1 FROM audit_events WHERE action='identity.delete' AND target=$1) AND $2<>''`,
		`SELECT count(*)=2 FROM outbox WHERE (payload->>'subject'=$1 OR payload->'link'->>'subject'=$1) AND $2<>''`,
	}
	for _, query := range checks {
		var ok bool
		if err = s.DB.QueryRow(ctx, query, u.ID, u.Email).Scan(&ok); err != nil || !ok {
			t.Fatalf("delete invariant failed: %s (%v)", query, err)
		}
	}
	newUser, err := s.CreateIdentity(ctx, u.Email, "New User", "new unrelated long user password", "member")
	if err != nil || newUser.ID == u.ID {
		t.Fatal("deleted email cannot be reused by a new subject")
	}
}

func TestConcurrentUserMutationsProtectLastAdmin(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	a, err := s.CreateIdentity(ctx, "first-admin@example.test", "First", "first administrator password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateIdentity(ctx, "second-admin@example.test", "Second", "second administrator password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteIdentity(ctx, a.ID, a.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatal("self deletion accepted")
	}
	results := make(chan error, 2)
	go func() { _, err := s.UpdateIdentity(ctx, "operator", a.ID, a.Email, a.Name, "member"); results <- err }()
	go func() { results <- s.DeleteIdentity(ctx, "operator", b.ID) }()
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, core.ErrConflict)) || (second == nil && errors.Is(first, core.ErrConflict))) {
		t.Fatalf("last-admin mutation results: %v / %v", first, second)
	}
	var admins int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM identities WHERE role='admin' AND status='active' AND deleted_at IS NULL`).Scan(&admins); err != nil || admins != 1 {
		t.Fatal("concurrent operations removed the last administrator")
	}
}
