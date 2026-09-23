package core

import "context"

// Maintenance releases only abandoned, never-active reservations. Active links
// do not expire merely because an application cannot currently reach FastCAS.
func (s *Store) Maintenance(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(89004324)`); err != nil {
		return err
	}
	// Use the same intent -> link lock order as activation.
	if _, err = tx.Exec(ctx, `SELECT id FROM link_intents WHERE state IN ('pending','confirmed','prepared') AND expires_at<now() FOR UPDATE`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE account_links l SET state='revoked',version=version+1,revoked_at=now() FROM link_intents i WHERE i.id=l.id AND l.state='prepared' AND i.expires_at<now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE link_intents SET state='revoked' WHERE state IN ('pending','confirmed','prepared') AND expires_at<now()`); err != nil {
		return err
	}
	for _, query := range []string{
		`DELETE FROM auth_requests WHERE expires_at<now()-interval '1 day'`,
		`DELETE FROM access_tokens WHERE expires_at<now()-interval '1 day'`,
		`DELETE FROM rate_limits WHERE expires_at<now()-interval '1 day'`,
		`UPDATE identities SET pending_mfa_secret=NULL,pending_mfa_expires=NULL WHERE pending_mfa_expires<now()`,
		`DELETE FROM outbox WHERE id IN (SELECT id FROM outbox WHERE delivered_at<now()-interval '90 days' ORDER BY delivered_at,id LIMIT 1000)`,
	} {
		if _, err = tx.Exec(ctx, query); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
