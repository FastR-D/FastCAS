package core

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type LinkIntent struct {
	ID        string    `json:"id"`
	ClientID  string    `json:"client_id"`
	LocalRef  string    `json:"local_account_ref"`
	Nonce     string    `json:"nonce"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}
type AccountLink struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	LocalRef   string    `json:"local_account_ref"`
	Subject    string    `json:"subject"`
	State      string    `json:"state"`
	Version    int64     `json:"version"`
	VerifiedAt time.Time `json:"verified_at"`
}

func (s *Store) CreateLinkIntent(ctx context.Context, client, localRef, idempotency string) (*LinkIntent, error) {
	if localRef == "" || len(localRef) > 256 || len(idempotency) < 16 || len(idempotency) > 128 {
		return nil, errors.New("local account and idempotency key required")
	}
	c, err := s.Client(ctx, client)
	if err != nil {
		return nil, err
	}
	if c.Public {
		return nil, ErrForbidden
	}
	intent := &LinkIntent{ID: RandomToken(), ClientID: client, LocalRef: localRef, Nonce: RandomToken(), State: "pending", ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	var payloadHash string
	err = s.DB.QueryRow(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(client_id,idempotency_key) DO UPDATE SET idempotency_key=excluded.idempotency_key
 RETURNING id,client_id,local_ref,nonce,state,expires_at,payload_hash`, intent.ID, client, localRef, idempotency, Hash(localRef), intent.Nonce, intent.ExpiresAt).Scan(&intent.ID, &intent.ClientID, &intent.LocalRef, &intent.Nonce, &intent.State, &intent.ExpiresAt, &payloadHash)
	if err != nil {
		return nil, err
	}
	if payloadHash != Hash(localRef) {
		return nil, ErrConflict
	}
	if intent.ExpiresAt.Before(time.Now()) {
		return nil, ErrExpired
	}
	return intent, nil
}

// IntentForAuthorization only exposes the local reference to the authenticated consent UI.
func (s *Store) IntentForAuthorization(ctx context.Context, a *Authorization) (*LinkIntent, error) {
	var i LinkIntent
	err := s.DB.QueryRow(ctx, `SELECT id,client_id,local_ref,nonce,state,expires_at FROM link_intents WHERE client_id=$1 AND nonce=$2 AND state='pending' AND expires_at>now()`, a.GetClientID(), a.GetNonce()).Scan(&i.ID, &i.ClientID, &i.LocalRef, &i.Nonce, &i.State, &i.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &i, err
}

func (s *Store) PrepareLink(ctx context.Context, client, id, nonce, subject string) (*AccountLink, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var local, storedNonce, state, confirmed string
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT local_ref,nonce,state,COALESCE(subject,''),expires_at FROM link_intents WHERE id=$1 AND client_id=$2 FOR UPDATE`, id, client).Scan(&local, &storedNonce, &state, &confirmed, &expires)
	if err != nil {
		return nil, ErrForbidden
	}
	if storedNonce != nonce || confirmed == "" || confirmed != subject {
		return nil, ErrForbidden
	}
	if state == "prepared" || state == "active" {
		return s.Link(ctx, client, id)
	}
	if state != "confirmed" || expires.Before(time.Now()) {
		return nil, ErrExpired
	}
	var link AccountLink
	err = tx.QueryRow(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES($1,$2,$3,$4,'prepared') RETURNING id,client_id,local_ref,subject,state,version,verified_at`, id, client, local, subject).Scan(&link.ID, &link.ClientID, &link.LocalRef, &link.Subject, &link.State, &link.Version, &link.VerifiedAt)
	if err != nil {
		return nil, classify(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE link_intents SET state='prepared' WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, subject, "link.prepare", id); err != nil {
		return nil, err
	}
	return &link, tx.Commit(ctx)
}
func (s *Store) ActivateLink(ctx context.Context, client, id string) (*AccountLink, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var state string
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT state,expires_at FROM link_intents WHERE id=$1 AND client_id=$2 FOR UPDATE`, id, client).Scan(&state, &expires)
	if err != nil {
		return nil, ErrForbidden
	}
	if state == "active" {
		return s.Link(ctx, client, id)
	}
	if state != "prepared" || expires.Before(time.Now()) {
		return nil, ErrExpired
	}
	var link AccountLink
	err = tx.QueryRow(ctx, `UPDATE account_links SET state='active',version=version+1 WHERE id=$1 AND client_id=$2 AND state='prepared' RETURNING id,client_id,local_ref,subject,state,version,verified_at`, id, client).Scan(&link.ID, &link.ClientID, &link.LocalRef, &link.Subject, &link.State, &link.Version, &link.VerifiedAt)
	if err != nil {
		return nil, ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE link_intents SET state='active' WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, link.Subject, "link.activate", id); err != nil {
		return nil, err
	}
	return &link, tx.Commit(ctx)
}
func (s *Store) Link(ctx context.Context, client, id string) (*AccountLink, error) {
	var link AccountLink
	err := s.DB.QueryRow(ctx, `SELECT id,client_id,local_ref,subject,state,version,verified_at FROM account_links WHERE id=$1 AND client_id=$2`, id, client).Scan(&link.ID, &link.ClientID, &link.LocalRef, &link.Subject, &link.State, &link.Version, &link.VerifiedAt)
	return &link, classify(err)
}
func (s *Store) ResolveLink(ctx context.Context, client, subject string) (*AccountLink, error) {
	var id string
	err := s.DB.QueryRow(ctx, `SELECT l.id FROM account_links l JOIN identities i ON i.id=l.subject WHERE l.client_id=$1 AND l.subject=$2 AND l.state='active' AND i.status='active'`, client, subject).Scan(&id)
	if err != nil {
		return nil, classify(err)
	}
	return s.Link(ctx, client, id)
}
func (s *Store) RevokeLink(ctx context.Context, client, id string, expected int64) (*AccountLink, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var link AccountLink
	err = tx.QueryRow(ctx, `SELECT id,client_id,local_ref,subject,state,version,verified_at FROM account_links WHERE id=$1 AND client_id=$2 FOR UPDATE`, id, client).Scan(&link.ID, &link.ClientID, &link.LocalRef, &link.Subject, &link.State, &link.Version, &link.VerifiedAt)
	if err != nil {
		return nil, ErrForbidden
	}
	if link.State == "revoked" {
		return &link, nil
	}
	if link.Version != expected {
		return nil, ErrConflict
	}
	link.Version++
	link.State = "revoked"
	if _, err = tx.Exec(ctx, `UPDATE account_links SET state='revoked',version=$2,revoked_at=now() WHERE id=$1`, id, link.Version); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE link_intents SET state='revoked' WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND client_id=$2`, link.Subject, client); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND client_id=$2`, link.Subject, client); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND payload->>'delegated'='true'
	 AND client_id IN (SELECT caller_client FROM exchange_policies WHERE target_client=$2)`, link.Subject, client); err != nil {
		return nil, err
	}
	event, err := marshal(map[string]any{"type": "account_link.revoked", "link": link})
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox(id,client_id,payload) VALUES($1,$2,$3)`, RandomToken(), client, event); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, client, "link.revoke", id); err != nil {
		return nil, err
	}
	return &link, tx.Commit(ctx)
}
