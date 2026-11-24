package core

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const importedEmailSuffix = "@fastresearch.invalid"

func (s *Store) IssueClaim(ctx context.Context, actor, target, email string) (string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	email, err := validEmail(email)
	if err != nil || strings.HasSuffix(email, importedEmailSuffix) {
		return "", ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT i.id FROM identities i JOIN fastresearch_imports f ON f.identity_id=i.id
 WHERE i.email=$1 AND i.role='member' AND i.status='active' AND i.deleted_at IS NULL
 AND i.password_hash='' AND i.mfa_secret IS NULL AND f.claimed_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM github_accounts WHERE identity_id=i.id) FOR UPDATE OF i`, target).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", err
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE email=$1)`, email).Scan(&occupied); err != nil {
		return "", err
	}
	if occupied {
		return "", ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE one_time_credentials SET consumed_at=now()
 WHERE identity_id=$1 AND kind='claim' AND consumed_at IS NULL`, id); err != nil {
		return "", err
	}
	raw := RandomToken()
	if _, err = tx.Exec(ctx, `INSERT INTO one_time_credentials(token_hash,kind,email,identity_id,expires_at)
 VALUES($1,'claim',$2,$3,now()+interval '1 hour')`, Hash(raw), email, id); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, actor, "credential.issue.claim", id); err != nil {
		return "", err
	}
	return raw, tx.Commit(ctx)
}

func (s *Store) RedeemClaim(ctx context.Context, raw, name, password string) (*Identity, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || raw == "" || len(raw) > 128 {
		return nil, ErrExpired
	}
	hash := Hash(raw)
	var id string
	err := s.DB.QueryRow(ctx, `SELECT c.identity_id FROM one_time_credentials c JOIN fastresearch_imports f ON f.identity_id=c.identity_id
 WHERE c.token_hash=$1 AND c.kind='claim' AND c.expires_at>now() AND c.consumed_at IS NULL
 AND f.claimed_at IS NULL AND f.legacy_name=$2`, hash, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, err
	}
	encoded, err := HashPassword(password)
	if err != nil {
		return nil, ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Issuance, redemption and account administration take the identity lock first.
	var locked string
	err = tx.QueryRow(ctx, `SELECT i.id FROM identities i JOIN fastresearch_imports f ON f.identity_id=i.id
 WHERE i.id=$1 AND i.role='member' AND i.status='active' AND i.deleted_at IS NULL
 AND i.password_hash='' AND i.mfa_secret IS NULL AND f.claimed_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM github_accounts WHERE identity_id=i.id) FOR UPDATE OF i`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, err
	}
	var email string
	err = tx.QueryRow(ctx, `UPDATE one_time_credentials SET consumed_at=now()
 WHERE token_hash=$1 AND identity_id=$2 AND kind='claim' AND expires_at>now() AND consumed_at IS NULL RETURNING email`, hash, id).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, err
	}
	var user Identity
	err = tx.QueryRow(ctx, `UPDATE identities SET email=$2,password_hash=$3,email_verified=false WHERE id=$1
 RETURNING id,email,name,role,status,email_verified`, id, email, encoded).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.Status, &user.EmailVerified)
	if err != nil {
		return nil, classify(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE fastresearch_imports SET claimed_at=now() WHERE identity_id=$1`, id); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, id, "credential.redeem.claim", id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &user, nil
}
