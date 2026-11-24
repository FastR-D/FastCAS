package core

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

const reusableInvitePrefix = "frinv_"

func IsReusableInvite(raw string) bool { return strings.HasPrefix(raw, reusableInvitePrefix) }

type ReusableInviteOptions struct {
	Operations     []string `json:"operations"`
	MaxUses        int      `json:"max_uses"`
	ExpiresInHours int      `json:"expires_in_hours"`
}

func (s *Store) IssueReusableInvite(ctx context.Context, actor string, options ReusableInviteOptions) (string, error) {
	if len(options.Operations) < 1 || len(options.Operations) > 2 ||
		options.MaxUses < 1 || options.MaxUses > 10000 || options.ExpiresInHours < 1 || options.ExpiresInHours > 720 {
		return "", ErrForbidden
	}
	var register, claim bool
	for _, operation := range options.Operations {
		switch {
		case operation == "register" && !register:
			register = true
		case operation == "claim" && !claim:
			claim = true
		default:
			return "", ErrForbidden
		}
	}
	raw := reusableInvitePrefix + RandomToken()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO reusable_invites(token_hash,allow_register,allow_claim,remaining_uses,expires_at)
 VALUES($1,$2,$3,$4,now()+make_interval(hours=>$5))`, Hash(raw), register, claim, options.MaxUses, options.ExpiresInHours); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, actor, "credential.issue.reusable_invite", "reusable_invite"); err != nil {
		return "", err
	}
	return raw, tx.Commit(ctx)
}

// The quota is shared by all allowed operations and rolls back with redemption.
func consumeReusableInvite(ctx context.Context, tx pgx.Tx, raw, operation string) error {
	if !IsReusableInvite(raw) || len(raw) != len(reusableInvitePrefix)+43 {
		return ErrExpired
	}
	result, err := tx.Exec(ctx, `UPDATE reusable_invites SET remaining_uses=remaining_uses-1
 WHERE token_hash=$1 AND expires_at>now() AND remaining_uses>0
 AND (($2='register' AND allow_register) OR ($2='claim' AND allow_claim))`, Hash(raw), operation)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrExpired
	}
	return nil
}

func (s *Store) RedeemReusableRegistration(ctx context.Context, raw, email, name, password string) (*Identity, error) {
	email, err := validEmail(email)
	name = strings.TrimSpace(name)
	if err != nil || strings.HasSuffix(email, importedEmailSuffix) || name == "" || len(name) > 200 {
		return nil, ErrForbidden
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
	if err = consumeReusableInvite(ctx, tx, raw, "register"); err != nil {
		return nil, err
	}
	user := &Identity{ID: RandomToken(), Email: email, Name: name, Role: "member", Status: "active"}
	if _, err = tx.Exec(ctx, `INSERT INTO identities(id,email,name,password_hash) VALUES($1,$2,$3,$4)`,
		user.ID, email, name, encoded); err != nil {
		return nil, classify(err)
	}
	if err = audit(ctx, tx, user.ID, "credential.redeem.reusable_registration", user.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return user, nil
}
