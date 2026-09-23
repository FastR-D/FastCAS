package core

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func queueLogout(ctx context.Context, tx pgx.Tx, subject, sid string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT l.client_id FROM account_links l JOIN applications a ON a.id=l.client_id
	 WHERE l.subject=$1 AND l.state='active' AND a.active`, subject)
	if err != nil {
		return err
	}
	clients := []string{}
	for rows.Next() {
		var client string
		if err = rows.Scan(&client); err != nil {
			rows.Close()
			return err
		}
		clients = append(clients, client)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	payload, err := marshal(map[string]string{"subject": subject, "sid": sid})
	if err != nil {
		return err
	}
	for _, client := range clients {
		if _, err = tx.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,kind) VALUES($1,$2,$3,'logout')`, RandomToken(), client, payload); err != nil {
			return err
		}
	}
	return nil
}

// LogoutAll revokes only the FastCAS identity's sessions and tokens. Local
// project credentials and business data are outside this transaction.
func (s *Store) LogoutAll(ctx context.Context, subject string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var active bool
	if err = tx.QueryRow(ctx, `SELECT status='active' FROM identities WHERE id=$1 FOR UPDATE`, subject).Scan(&active); err != nil || !active {
		return ErrUnauthorized
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
	if err = queueLogout(ctx, tx, subject, ""); err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "session.logout_all", subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LogoutSession terminates one browser session, its derived tokens and the
// matching application sessions identified by the OIDC sid claim.
func (s *Store) LogoutSession(ctx context.Context, subject, sid string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var live bool
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL FROM browser_sessions WHERE id=$1 AND identity_id=$2 FOR UPDATE`, sid, subject).Scan(&live); err != nil {
		return ErrUnauthorized
	}
	if !live {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE browser_sessions SET revoked_at=now() WHERE id=$1`, sid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND session_id=$2`, subject, sid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND payload->>'sid'=$2`, subject, sid); err != nil {
		return err
	}
	if err = queueLogout(ctx, tx, subject, sid); err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "session.logout", sid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
