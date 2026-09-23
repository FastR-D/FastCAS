package core

import "context"

// FenceRestoredState invalidates every credential and link in a restored snapshot.
// Run only while the issuer and delivery workers are stopped. Existing local
// project accounts remain untouched; users must explicitly link them again.
func (s *Store) FenceRestoredState(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(89004323)`); err != nil {
		return err
	}
	statements := []string{
		`UPDATE outbox SET dead_at=now(),lease_until=NULL,lease_token=NULL WHERE delivered_at IS NULL AND dead_at IS NULL`,
		`INSERT INTO outbox(id,client_id,payload,kind)
		 SELECT gen_random_uuid()::text,l.client_id,jsonb_build_object('subject',l.subject,'sid',''),'logout'
		 FROM (SELECT DISTINCT client_id,subject FROM account_links) l
		 JOIN applications a ON a.id=l.client_id AND a.active`,
		`UPDATE browser_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE revoked_at IS NULL`,
		`UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE revoked_at IS NULL`,
		`UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE revoked_at IS NULL`,
		`UPDATE device_installations SET revoked_at=COALESCE(revoked_at,now()) WHERE revoked_at IS NULL`,
		`UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE consumed_at IS NULL`,
		`UPDATE device_authorizations SET status='denied' WHERE status IN ('pending','approved')`,
		`UPDATE exchange_consents SET revoked_at=COALESCE(revoked_at,now()) WHERE revoked_at IS NULL`,
		`UPDATE one_time_credentials SET consumed_at=COALESCE(consumed_at,now()) WHERE consumed_at IS NULL`,
		`INSERT INTO outbox(id,client_id,payload)
		 SELECT gen_random_uuid()::text,l.client_id,jsonb_build_object('type','account_link.revoked',
		 'link',jsonb_build_object('id',l.id,'client_id',l.client_id,'local_account_ref',l.local_ref,
		 'subject',l.subject,'state','revoked','version',CASE WHEN l.state='revoked' THEN l.version ELSE l.version+1 END,'verified_at',l.verified_at))
		 FROM account_links l JOIN applications a ON a.id=l.client_id AND a.active`,
		`UPDATE account_links SET state='revoked',version=version+1,revoked_at=now() WHERE state<>'revoked'`,
		`UPDATE link_intents SET state='revoked' WHERE state<>'revoked'`,
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, "system", "recovery.fence", "all"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
