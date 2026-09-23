package core_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/pquerna/otp/totp"
)

func TestMFARequiredForAdminAndRecoveryCodesAreOneTime(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.CreateIdentity(ctx, "admin@example.test", "Admin", "correct horse battery staple", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if s.RequireAdmin(ctx, session) == nil {
		t.Fatal("administrator without MFA admitted")
	}
	uri, err := s.BeginMFA(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	secret := parsed.Query().Get("secret")
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.ConfirmMFA(ctx, u.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 8 {
		t.Fatal("recovery codes missing")
	}
	if s.RequireAdmin(ctx, session) != nil {
		t.Fatal("recent enrolled MFA not recognized")
	}
	if s.VerifyMFA(ctx, u.ID, code) == nil {
		t.Fatal("TOTP replay accepted")
	}
	if err = s.VerifyMFA(ctx, u.ID, codes[0]); err != nil {
		t.Fatal(err)
	}
	if s.VerifyMFA(ctx, u.ID, codes[0]) == nil {
		t.Fatal("recovery code replay accepted")
	}
	if err = s.SetIdentityStatus(ctx, u.ID, u.ID, "disabled"); !errors.Is(err, core.ErrConflict) {
		t.Fatal("last administrator disabled")
	}
	if _, err = s.BeginMFA(ctx, u.ID); !errors.Is(err, core.ErrConflict) {
		t.Fatal("existing MFA overwritten without proof")
	}
}

func TestRotateRecoveryCodesRequiresRecentPasswordAndMFA(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.CreateIdentity(ctx, "rotation@example.test", "Rotation", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := s.BeginMFA(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.ConfirmMFA(ctx, u.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Session(ctx, otherRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateMFARecoveryCodes(ctx, u.ID, other.ID); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("session without recent MFA rotated recovery codes")
	}
	newCodes, err := s.RotateMFARecoveryCodes(ctx, u.ID, session.ID)
	if err != nil || len(newCodes) != 8 || newCodes[0] == old[0] {
		t.Fatalf("rotation failed: %d %v", len(newCodes), err)
	}
	if err = s.VerifyMFA(ctx, u.ID, old[0]); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old recovery code survived rotation")
	}
	if err = s.VerifyMFA(ctx, u.ID, newCodes[0]); err != nil {
		t.Fatal(err)
	}
	if err = s.VerifyMFA(ctx, u.ID, newCodes[0]); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("new recovery code was reusable")
	}
	if _, err = s.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now()-interval '6 minutes' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateMFARecoveryCodes(ctx, u.ID, session.ID); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("stale MFA session rotated recovery codes")
	}
	var auditCount int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='mfa.recovery_codes.rotate' AND actor=$1`, u.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("expected one rotation audit: %d %v", auditCount, err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_rotate_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='mfa.recovery_codes.rotate' THEN RAISE EXCEPTION 'audit unavailable'; END IF;
 RETURN NEW; END $$;
	CREATE TRIGGER reject_rotate_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_rotate_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateMFARecoveryCodes(ctx, u.ID, session.ID); err == nil {
		t.Fatal("rotation committed without audit")
	}
	if err = s.VerifyMFA(ctx, u.ID, newCodes[1]); err != nil {
		t.Fatalf("audit rollback destroyed previous recovery codes: %v", err)
	}
}
func TestAdministratorIssuedMFAResetRequiresCurrentPasswordAndLogsOut(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.CreateIdentity(ctx, "lost-device@example.test", "Lost device", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	rawSession, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, rawSession)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := s.BeginMFA(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	oldCodes, err := s.ConfirmMFA(ctx, u.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true,
		Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
 VALUES('mfa-reset-link','receiver','local-user','mfa-reset-key','hash','nonce',$1,'active',now()+interval '1 hour')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state)
 VALUES('mfa-reset-link','receiver','local-user',$1,'active')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO auth_requests(id,client_id,payload,expires_at,code_hash,code_expires_at)
 VALUES('mfa-reset-code','receiver',jsonb_build_object('Subject',$1::text),now()+interval '1 hour','mfa-reset-code-hash',now()+interval '1 hour')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO device_authorizations(device_code_hash,user_code,client_id,scopes,expires_at,status,subject)
 VALUES('mfa-reset-device','RESET-CODE','receiver','[]'::jsonb,now()+interval '1 hour','approved',$1)`, u.ID); err != nil {
		t.Fatal(err)
	}
	credential, err := s.IssueCredential(ctx, "admin", "mfa_reset", u.Email)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RedeemMFAReset(ctx, credential, "wrong password", "reset-test"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("wrong password reset MFA")
	}
	if enabled, err := s.MFAEnabled(ctx, u.ID); err != nil || !enabled {
		t.Fatal("wrong password consumed reset credential")
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_mfa_reset_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='mfa.reset' THEN RAISE EXCEPTION 'audit unavailable'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_mfa_reset_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_mfa_reset_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = s.RedeemMFAReset(ctx, credential, "correct horse battery staple", "reset-test"); err == nil {
		t.Fatal("MFA reset committed without audit")
	}
	if enabled, err := s.MFAEnabled(ctx, u.ID); err != nil || !enabled {
		t.Fatal("audit failure cleared MFA")
	}
	if _, err = s.Session(ctx, rawSession); err != nil {
		t.Fatal("audit failure revoked browser session")
	}
	if _, err = s.DB.Exec(ctx, `DROP TRIGGER reject_mfa_reset_audit ON audit_events; DROP FUNCTION reject_mfa_reset_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = s.RedeemMFAReset(ctx, credential, "correct horse battery staple", "reset-test"); err != nil {
		t.Fatal(err)
	}
	if enabled, err := s.MFAEnabled(ctx, u.ID); err != nil || enabled {
		t.Fatal("MFA remained enabled after reset")
	}
	if _, err = s.Session(ctx, rawSession); err == nil {
		t.Fatal("old browser session survived MFA reset")
	}
	if err = s.VerifyMFA(ctx, u.ID, oldCodes[0]); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old recovery code survived MFA reset")
	}
	if err = s.RedeemMFAReset(ctx, credential, "correct horse battery staple", "reset-test"); !errors.Is(err, core.ErrExpired) {
		t.Fatal("MFA reset credential replay accepted")
	}
	if _, err = s.Authenticate(ctx, u.Email, "correct horse battery staple", "post-reset"); err != nil {
		t.Fatal("MFA reset unexpectedly changed password")
	}
	var queued, audits int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE client_id='receiver' AND kind='logout'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("missing project logout notice: %d %v", queued, err)
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='mfa.reset' AND actor=$1`, u.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("missing MFA reset audit: %d %v", audits, err)
	}
	var codeConsumed bool
	if err = s.DB.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM auth_requests WHERE id='mfa-reset-code'`).Scan(&codeConsumed); err != nil || !codeConsumed {
		t.Fatal("approved authorization code survived MFA reset")
	}
	var deviceStatus string
	if err = s.DB.QueryRow(ctx, `SELECT status FROM device_authorizations WHERE device_code_hash='mfa-reset-device'`).Scan(&deviceStatus); err != nil || deviceStatus != "denied" {
		t.Fatalf("approved device authorization survived MFA reset: %s %v", deviceStatus, err)
	}
}

func TestChangePasswordRequiresCurrentPasswordAndMFAAndRevokesFastCASSessions(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.CreateIdentity(ctx, "change@example.test", "Change", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := s.BeginMFA(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.ConfirmMFA(ctx, u.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterClient(ctx, core.Client{ID: "change-receiver", Name: "Receiver", Development: true,
		Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
 VALUES('change-link','change-receiver','local-user','change-key','hash','nonce',$1,'active',now()+interval '1 hour')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state)
 VALUES('change-link','change-receiver','local-user',$1,'active')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO auth_requests(id,client_id,payload,expires_at,code_hash,code_expires_at)
 VALUES('change-code','change-receiver',jsonb_build_object('Subject',$1::text),now()+interval '1 hour','change-code-hash',now()+interval '1 hour')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO device_authorizations(device_code_hash,user_code,client_id,scopes,expires_at,status,subject)
 VALUES('change-device','CHANGE-CODE','change-receiver','[]'::jsonb,now()+interval '1 hour','approved',$1)`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, u.ID, session.ID, "wrong password", "new password long enough", codes[0], "change-test"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("wrong password accepted: %v", err)
	}
	if err = s.ChangePassword(ctx, u.ID, session.ID, "correct horse battery staple", "new password long enough", "bad", "change-test"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("missing MFA accepted: %v", err)
	}
	if err = s.ChangePassword(ctx, u.ID, session.ID, "correct horse battery staple", "new password long enough", codes[0], "change-test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, raw); err == nil {
		t.Fatal("current session survived")
	}
	if _, err = s.Session(ctx, other); err == nil {
		t.Fatal("other session survived")
	}
	if _, err = s.Authenticate(ctx, u.Email, "correct horse battery staple", "after-change"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old password still valid")
	}
	updated, err := s.Authenticate(ctx, u.Email, "new password long enough", "after-change")
	if err != nil || updated.ID != u.ID {
		t.Fatalf("new password did not preserve identity: %v", err)
	}
	var queued, audited int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE client_id='change-receiver' AND kind='logout'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("logout not queued: %d %v", queued, err)
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='password.change' AND actor=$1`, u.ID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit missing: %d %v", audited, err)
	}
	var consumed bool
	if err = s.DB.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM auth_requests WHERE id='change-code'`).Scan(&consumed); err != nil || !consumed {
		t.Fatal("pending authorization survived")
	}
	var deviceStatus string
	if err = s.DB.QueryRow(ctx, `SELECT status FROM device_authorizations WHERE device_code_hash='change-device'`).Scan(&deviceStatus); err != nil || deviceStatus != "denied" {
		t.Fatal("approved device authorization survived")
	}
}

func TestInvitationAndRecoveryPreserveIdentityAndRevokeSessions(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	invite, err := s.IssueCredential(ctx, "admin", "invite", "new@example.test")
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.RedeemCredential(ctx, "invite", invite, "New User", "original long password")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != "member" {
		t.Fatal("invitation created administrator")
	}
	if _, err = s.RedeemCredential(ctx, "invite", invite, "Duplicate", "original long password"); err == nil {
		t.Fatal("invite reused")
	}
	raw, _, err := s.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterClient(ctx, core.Client{ID: "recovery-receiver", Name: "Receiver", Development: true,
		Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
 VALUES('recovery-link','recovery-receiver','local-user','recovery-key','hash','nonce',$1,'active',now()+interval '1 hour')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state)
 VALUES('recovery-link','recovery-receiver','local-user',$1,'active')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO auth_requests(id,client_id,payload,expires_at,code_hash,code_expires_at)
 VALUES('recovery-code','recovery-receiver',jsonb_build_object('Subject',$1::text),now()+interval '1 hour','recovery-code-hash',now()+interval '1 hour')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO device_authorizations(device_code_hash,user_code,client_id,scopes,expires_at,status,subject)
 VALUES('recovery-device','RECOVERY-CODE','recovery-receiver','[]'::jsonb,now()+interval '1 hour','approved',$1)`, user.ID); err != nil {
		t.Fatal(err)
	}
	recovery, err := s.IssueCredential(ctx, "admin", "recovery", user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_recovery_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='credential.redeem.recovery' THEN RAISE EXCEPTION 'audit unavailable'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_recovery_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_recovery_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemCredential(ctx, "recovery", recovery, "ignored", "replacement long password"); err == nil {
		t.Fatal("recovery committed without audit")
	}
	if _, err = s.Session(ctx, raw); err != nil {
		t.Fatal("audit failure revoked old session")
	}
	if _, err = s.Authenticate(ctx, user.Email, "original long password", "before-recovery"); err != nil {
		t.Fatal("audit failure changed password")
	}
	if _, err = s.DB.Exec(ctx, `DROP TRIGGER reject_recovery_audit ON audit_events; DROP FUNCTION reject_recovery_audit()`); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.RedeemCredential(ctx, "recovery", recovery, "ignored", "replacement long password")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != user.ID {
		t.Fatal("recovery changed identity")
	}
	if _, err = s.Session(ctx, raw); err == nil {
		t.Fatal("pre-recovery session survived")
	}
	if _, err = s.Authenticate(ctx, user.Email, "replacement long password", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, user.Email, "original long password", "test"); err == nil {
		t.Fatal("old password survived recovery")
	}
	var queued, audited int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE client_id='recovery-receiver' AND kind='logout'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("recovery logout not queued: %d %v", queued, err)
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='credential.redeem.recovery' AND actor=$1`, user.ID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("recovery audit missing: %d %v", audited, err)
	}
	var consumed bool
	if err = s.DB.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM auth_requests WHERE id='recovery-code'`).Scan(&consumed); err != nil || !consumed {
		t.Fatal("pending authorization survived password recovery")
	}
	var deviceStatus string
	if err = s.DB.QueryRow(ctx, `SELECT status FROM device_authorizations WHERE device_code_hash='recovery-device'`).Scan(&deviceStatus); err != nil || deviceStatus != "denied" {
		t.Fatal("approved device authorization survived password recovery")
	}
}
