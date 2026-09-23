package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func (s *Store) encrypt(plain string) ([]byte, error) {
	if s.Keys == nil {
		return nil, errors.New("encryption key unavailable")
	}
	block, err := aes.NewCipher(s.Keys.Encryption[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), []byte("fastcas:mfa:v1")), nil
}
func (s *Store) decrypt(encrypted []byte) (string, error) {
	if s.Keys == nil {
		return "", errors.New("encryption key unavailable")
	}
	block, err := aes.NewCipher(s.Keys.Encryption[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(encrypted) < gcm.NonceSize() {
		return "", ErrUnauthorized
	}
	nonce := encrypted[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, encrypted[gcm.NonceSize():], []byte("fastcas:mfa:v1"))
	return string(plain), err
}

func (s *Store) MFAEnabled(ctx context.Context, subject string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRow(ctx, `SELECT mfa_secret IS NOT NULL FROM identities WHERE id=$1 AND status='active'`, subject).Scan(&enabled)
	return enabled, classify(err)
}
func (s *Store) BeginMFA(ctx context.Context, subject string) (string, error) {
	u, err := s.Identity(ctx, subject)
	if err != nil {
		return "", err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "FastCAS", AccountName: u.Email, SecretSize: 20, Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", err
	}
	sealed, err := s.encrypt(key.Secret())
	if err != nil {
		return "", err
	}
	result, err := s.DB.Exec(ctx, `UPDATE identities SET pending_mfa_secret=$2,pending_mfa_expires=now()+interval '5 minutes' WHERE id=$1 AND mfa_secret IS NULL AND status='active'`, subject, sealed)
	if err != nil {
		return "", err
	}
	if result.RowsAffected() != 1 {
		return "", ErrConflict
	}
	return key.URL(), nil
}
func totpStep(secret, code string) int64 {
	if len(code) != 6 {
		return -1
	}
	now := time.Now().Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		value, err := totp.GenerateCodeCustom(secret, time.Unix((now+offset)*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(value), []byte(code)) == 1 {
			return now + offset
		}
	}
	return -1
}
func (s *Store) ConfirmMFA(ctx context.Context, subject, sessionID, code string) ([]string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT pending_mfa_secret FROM identities WHERE id=$1 AND mfa_secret IS NULL AND pending_mfa_expires>now() AND status='active' FOR UPDATE`, subject).Scan(&sealed)
	if err != nil {
		return nil, ErrExpired
	}
	secret, err := s.decrypt(sealed)
	if err != nil {
		return nil, err
	}
	step := totpStep(secret, code)
	if step < 0 {
		return nil, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE identities SET mfa_secret=pending_mfa_secret,pending_mfa_secret=NULL,pending_mfa_expires=NULL,mfa_last_step=$2 WHERE id=$1`, subject, step); err != nil {
		return nil, err
	}
	codes := make([]string, 8)
	for i := range codes {
		codes[i] = RandomToken()
		if _, err = tx.Exec(ctx, `INSERT INTO recovery_codes(identity_id,token_hash) VALUES($1,$2)`, subject, Hash(codes[i])); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE id=$1 AND identity_id=$2 AND revoked_at IS NULL`, sessionID, subject); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=now() WHERE identity_id=$1 AND id<>$2 AND revoked_at IS NULL`, subject, sessionID); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, subject, "mfa.enroll", subject); err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}

// RotateMFARecoveryCodes replaces every existing code only after recent
// password and MFA verification on the same live browser session.
func (s *Store) RotateMFARecoveryCodes(ctx context.Context, subject, sessionID string) ([]string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT i.mfa_secret IS NOT NULL FROM identities i
 JOIN browser_sessions b ON b.identity_id=i.id WHERE i.id=$1 AND b.id=$2
 AND i.status='active' AND b.revoked_at IS NULL AND b.expires_at>now()
 AND b.authenticated_at>now()-interval '5 minutes' AND b.mfa_at>now()-interval '5 minutes'
 FOR UPDATE OF i`, subject, sessionID).Scan(&enabled)
	if err != nil || !enabled {
		return nil, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE identity_id=$1`, subject); err != nil {
		return nil, err
	}
	codes := make([]string, 8)
	for i := range codes {
		codes[i] = RandomToken()
		if _, err = tx.Exec(ctx, `INSERT INTO recovery_codes(identity_id,token_hash) VALUES($1,$2)`, subject, Hash(codes[i])); err != nil {
			return nil, err
		}
	}
	if err = audit(ctx, tx, subject, "mfa.recovery_codes.rotate", subject); err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}
func (s *Store) VerifyMFA(ctx context.Context, subject, code string) error {
	allowed, err := s.Allow(ctx, "mfa:"+subject, 10, 5*time.Minute)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrUnauthorized
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sealed []byte
	var last int64
	err = tx.QueryRow(ctx, `SELECT mfa_secret,mfa_last_step FROM identities WHERE id=$1 AND status='active' FOR UPDATE`, subject).Scan(&sealed, &last)
	if err != nil || len(sealed) == 0 {
		return ErrUnauthorized
	}
	if len(code) > 6 {
		result, err := tx.Exec(ctx, `UPDATE recovery_codes SET consumed_at=now() WHERE identity_id=$1 AND token_hash=$2 AND consumed_at IS NULL`, subject, Hash(strings.TrimSpace(code)))
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrUnauthorized
		}
		if err = audit(ctx, tx, subject, "mfa.recovery_code", subject); err != nil {
			return err
		}
	} else {
		secret, err := s.decrypt(sealed)
		if err != nil {
			return err
		}
		step := totpStep(secret, code)
		if step < 0 || step <= last {
			return ErrUnauthorized
		}
		if _, err = tx.Exec(ctx, `UPDATE identities SET mfa_last_step=$2 WHERE id=$1`, subject, step); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) MarkMFA(ctx context.Context, subject, sessionID string) error {
	result, err := s.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE id=$1 AND identity_id=$2 AND revoked_at IS NULL AND expires_at>now()`, sessionID, subject)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrUnauthorized
	}
	return nil
}
func (s *Store) RequireAdmin(ctx context.Context, session *BrowserSession) error {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities i JOIN browser_sessions s ON s.identity_id=i.id WHERE i.id=$1 AND i.role='admin' AND i.status='active' AND i.mfa_secret IS NOT NULL AND s.id=$2 AND s.mfa_at>now()-interval '5 minutes' AND s.revoked_at IS NULL AND s.expires_at>now())`, session.IdentityID, session.ID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// One-time invitations and recovery tokens are delivered by an administrator.
// They are not queryable after creation and never go into audit payloads.
func (s *Store) IssueCredential(ctx context.Context, actor, kind, email string) (string, error) {
	if kind != "invite" && kind != "recovery" && kind != "mfa_reset" {
		return "", ErrForbidden
	}
	email, err := validEmail(email)
	if err != nil {
		return "", err
	}
	raw := RandomToken()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if kind == "recovery" || kind == "mfa_reset" {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE email=$1 AND status='active' AND ($2<>'mfa_reset' OR mfa_secret IS NOT NULL))`, email, kind).Scan(&active); err != nil {
			return "", err
		}
		if !active {
			return "", ErrForbidden
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE one_time_credentials SET consumed_at=now() WHERE email=$1 AND kind=$2 AND consumed_at IS NULL`, email, kind); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO one_time_credentials(token_hash,kind,email,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, Hash(raw), kind, email); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, actor, "credential.issue."+kind, email); err != nil {
		return "", err
	}
	return raw, tx.Commit(ctx)
}

// RedeemMFAReset requires both the administrator-issued one-time credential
// and the account's current password. It clears only FastCAS MFA credentials,
// then revokes FastCAS sessions/tokens and queues project logout notices.
func (s *Store) RedeemMFAReset(ctx context.Context, raw, password, peer string) error {
	var email string
	err := s.DB.QueryRow(ctx, `SELECT email FROM one_time_credentials WHERE token_hash=$1 AND kind='mfa_reset' AND expires_at>now() AND consumed_at IS NULL`, Hash(raw)).Scan(&email)
	if err != nil {
		return ErrExpired
	}
	if _, err = s.Authenticate(ctx, email, password, peer); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE one_time_credentials SET consumed_at=now()
 WHERE token_hash=$1 AND kind='mfa_reset' AND expires_at>now() AND consumed_at IS NULL RETURNING email`, Hash(raw)).Scan(&email)
	if err != nil {
		return ErrExpired
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM identities WHERE email=$1 AND status='active' AND mfa_secret IS NOT NULL FOR UPDATE`, email).Scan(&id)
	if err != nil {
		return ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE identities SET mfa_secret=NULL,pending_mfa_secret=NULL,pending_mfa_expires=NULL,mfa_last_step=-1 WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE identity_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE identity_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE payload->>'Subject'=$1 AND consumed_at IS NULL`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE device_authorizations SET status='denied' WHERE subject=$1 AND status='approved'`, id); err != nil {
		return err
	}
	if err = queueLogout(ctx, tx, id, ""); err != nil {
		return err
	}
	if err = audit(ctx, tx, id, "mfa.reset", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ChangePassword changes only the FastCAS credential. Existing local project
// credentials remain owned by each application.
func (s *Store) ChangePassword(ctx context.Context, subject, sessionID, current, next, code, peer string) error {
	if current == next {
		return ErrConflict
	}
	encoded, err := HashPassword(next)
	if err != nil {
		return ErrForbidden
	}
	u, err := s.Identity(ctx, subject)
	if err != nil {
		return err
	}
	if _, err = s.Authenticate(ctx, u.Email, current, peer); err != nil {
		return err
	}
	enabled, err := s.MFAEnabled(ctx, subject)
	if err != nil {
		return err
	}
	if enabled {
		if err = s.VerifyMFA(ctx, subject, code); err != nil {
			return err
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oldHash string
	var stillEnabled bool
	err = tx.QueryRow(ctx, `SELECT i.password_hash,i.mfa_secret IS NOT NULL FROM identities i
 JOIN browser_sessions b ON b.identity_id=i.id WHERE i.id=$1 AND b.id=$2
 AND i.status='active' AND b.revoked_at IS NULL AND b.expires_at>now()
 FOR UPDATE OF i,b`, subject, sessionID).Scan(&oldHash, &stillEnabled)
	if err != nil || stillEnabled != enabled || !PasswordMatches(current, oldHash) {
		return ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE identities SET password_hash=$2 WHERE id=$1`, subject, encoded); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE identity_id=$1`, subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE payload->>'Subject'=$1 AND consumed_at IS NULL`, subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE device_authorizations SET status='denied' WHERE subject=$1 AND status='approved'`, subject); err != nil {
		return err
	}
	if err = queueLogout(ctx, tx, subject, ""); err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "password.change", subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RedeemCredential(ctx context.Context, kind, raw, name, password string) (*Identity, error) {
	if kind != "invite" && kind != "recovery" {
		return nil, ErrForbidden
	}
	encoded, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var email string
	err = tx.QueryRow(ctx, `UPDATE one_time_credentials SET consumed_at=now() WHERE token_hash=$1 AND kind=$2 AND expires_at>now() AND consumed_at IS NULL RETURNING email`, Hash(raw), kind).Scan(&email)
	if err != nil {
		return nil, ErrExpired
	}
	var id string
	if kind == "invite" {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 200 {
			return nil, ErrForbidden
		}
		id = RandomToken()
		_, err = tx.Exec(ctx, `INSERT INTO identities(id,email,name,password_hash) VALUES($1,$2,$3,$4)`, id, email, name, encoded)
	} else {
		err = tx.QueryRow(ctx, `UPDATE identities SET password_hash=$2 WHERE email=$1 AND status='active' RETURNING id`, email, encoded).Scan(&id)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE identity_id=$1`, id)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE payload->>'Subject'=$1 AND consumed_at IS NULL`, id)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE device_authorizations SET status='denied' WHERE subject=$1 AND status='approved'`, id)
		}
		if err == nil {
			err = queueLogout(ctx, tx, id, "")
		}
	}
	if err != nil {
		return nil, classify(err)
	}
	if err = audit(ctx, tx, id, "credential.redeem."+kind, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Identity(ctx, id)
}

func (s *Store) SetIdentityStatus(ctx context.Context, actor, id, status string) error {
	if status != "active" && status != "disabled" {
		return ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(89004323)`); err != nil {
		return err
	}
	var role, current string
	if err = tx.QueryRow(ctx, `SELECT role,status FROM identities WHERE id=$1 FOR UPDATE`, id).Scan(&role, &current); err != nil {
		return ErrForbidden
	}
	if current == status {
		return nil
	}
	if status == "disabled" && role == "admin" {
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM identities WHERE role='admin' AND status='active' AND id<>$1`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrConflict
		}
	}
	var version int64
	if err = tx.QueryRow(ctx, `UPDATE identities SET status=$2,status_version=status_version+1 WHERE id=$1 RETURNING status_version`, id, status).Scan(&version); err != nil {
		return err
	}
	payload, err := marshal(map[string]any{"type": "identity.status_changed", "subject": id, "status": status, "version": version})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox(id,client_id,payload)
	 SELECT gen_random_uuid()::text,l.client_id,$2::jsonb FROM
	 (SELECT DISTINCT client_id FROM account_links WHERE subject=$1 AND state='active') l
	 JOIN applications a ON a.id=l.client_id AND a.active`, id, payload); err != nil {
		return err
	}
	if status == "disabled" {
		if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE identity_id=$1`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`, id); err != nil {
			return err
		}
		if err = queueLogout(ctx, tx, id, ""); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, actor, "identity."+status, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
