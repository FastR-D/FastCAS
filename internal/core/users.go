package core

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ManagedIdentity struct {
	Identity
	CreatedAt  time.Time `json:"created_at"`
	MFAEnabled bool      `json:"mfa_enabled"`
}

// Use the same lock as status changes so concurrent demotions/deletions cannot
// remove the last active administrator.
func protectLastAdmin(ctx context.Context, tx pgx.Tx, id, role, status string) error {
	if role != "admin" || status != "active" {
		return nil
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM identities WHERE id<>$1 AND role='admin' AND status='active' AND deleted_at IS NULL`, id).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		return ErrConflict
	}
	return nil
}

func revokeIdentityAccess(ctx context.Context, tx pgx.Tx, id string) error {
	for _, statement := range []string{
		`UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE identity_id=$1`,
		`UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`,
		`UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`,
		`UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE payload->>'Subject'=$1`,
		`UPDATE device_authorizations SET status='denied' WHERE subject=$1 AND status IN ('pending','approved')`,
	} {
		if _, err := tx.Exec(ctx, statement, id); err != nil {
			return err
		}
	}
	return queueLogout(ctx, tx, id, "")
}

func (s *Store) UpdateIdentity(ctx context.Context, actor, id, email, name, role string) (*ManagedIdentity, error) {
	email, err := validEmail(email)
	name = strings.TrimSpace(name)
	if err != nil || name == "" || len(name) > 200 || (role != "member" && role != "admin") {
		return nil, ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(89004323)`); err != nil {
		return nil, err
	}
	var oldEmail, oldRole, status string
	if err = tx.QueryRow(ctx, `SELECT email,role,status FROM identities WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldEmail, &oldRole, &status); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrForbidden
		}
		return nil, err
	}
	if oldRole != role {
		if actor == id {
			return nil, ErrForbidden
		}
		if err = protectLastAdmin(ctx, tx, id, oldRole, status); err != nil {
			return nil, err
		}
	}
	var updated ManagedIdentity
	err = tx.QueryRow(ctx, `UPDATE identities SET name=$2,email=$3,role=$4,email_verified=CASE WHEN email=$3 THEN email_verified ELSE false END
	 WHERE id=$1 RETURNING id,email,name,role,status,email_verified,created_at,mfa_secret IS NOT NULL`, id, name, email, role).Scan(
		&updated.ID, &updated.Email, &updated.Name, &updated.Role, &updated.Status, &updated.EmailVerified, &updated.CreatedAt, &updated.MFAEnabled)
	if err != nil {
		return nil, classify(err)
	}
	if oldEmail != email || oldRole != role {
		if _, err = tx.Exec(ctx, `UPDATE one_time_credentials SET consumed_at=COALESCE(consumed_at,now()) WHERE email=$1 OR email=$2`, oldEmail, email); err != nil {
			return nil, err
		}
		if err = revokeIdentityAccess(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if err = audit(ctx, tx, actor, "identity.update", id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

// Retain a tombstone for referenced subjects and audit history. The deleted
// account cannot be restored; its email can be used by a new, unrelated account.
func (s *Store) DeleteIdentity(ctx context.Context, actor, id string) error {
	if actor == id {
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
	var email, role, status string
	if err = tx.QueryRow(ctx, `SELECT email,role,status FROM identities WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&email, &role, &status); err != nil {
		if err == pgx.ErrNoRows {
			return ErrForbidden
		}
		return err
	}
	if err = protectLastAdmin(ctx, tx, id, role, status); err != nil {
		return err
	}
	if err = revokeIdentityAccess(ctx, tx, id); err != nil {
		return err
	}
	statements := []string{
		`INSERT INTO outbox(id,client_id,payload)
		 SELECT gen_random_uuid()::text,l.client_id,jsonb_build_object('type','account_link.revoked',
		 'link',jsonb_build_object('id',l.id,'client_id',l.client_id,'local_account_ref',l.local_ref,
		 'subject',l.subject,'state','revoked','version',l.version+1,'verified_at',l.verified_at))
		 FROM account_links l JOIN applications a ON a.id=l.client_id AND a.active WHERE l.subject=$1 AND l.state<>'revoked'`,
		`UPDATE account_links SET state='revoked',version=version+1,revoked_at=now() WHERE subject=$1 AND state<>'revoked'`,
		`UPDATE link_intents SET state='revoked' WHERE subject=$1 AND state<>'revoked'`,
		`UPDATE device_installations SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`,
		`UPDATE exchange_consents SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1`,
		`DELETE FROM recovery_codes WHERE identity_id=$1`,
		`UPDATE identities SET deleted_at=now(),status='disabled',status_version=status_version+1,
		 email='deleted-'||id||'@deleted.invalid',name='已删除用户',role='member',password_hash='',email_verified=false,
		 mfa_secret=NULL,pending_mfa_secret=NULL,pending_mfa_expires=NULL,mfa_last_step=-1 WHERE id=$1`,
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement, id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE one_time_credentials SET consumed_at=COALESCE(consumed_at,now()) WHERE email=$1`, email); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "identity.delete", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
