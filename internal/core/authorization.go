package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type Authorization struct {
	ID            string
	CreatedAt     time.Time
	Request       oidc.AuthRequest
	Subject       string
	SessionID     string
	AuthTime      time.Time
	Authenticated bool
}

func (a *Authorization) GetID() string          { return a.ID }
func (a *Authorization) GetACR() string         { return "" }
func (a *Authorization) GetAMR() []string       { return []string{"pwd"} }
func (a *Authorization) GetAudience() []string  { return []string{a.Request.ClientID} }
func (a *Authorization) GetAuthTime() time.Time { return a.AuthTime }
func (a *Authorization) GetClientID() string    { return a.Request.ClientID }
func (a *Authorization) GetCodeChallenge() *oidc.CodeChallenge {
	return &oidc.CodeChallenge{Challenge: a.Request.CodeChallenge, Method: a.Request.CodeChallengeMethod}
}
func (a *Authorization) GetNonce() string                   { return a.Request.Nonce }
func (a *Authorization) GetRedirectURI() string             { return a.Request.RedirectURI }
func (a *Authorization) GetResponseType() oidc.ResponseType { return a.Request.ResponseType }
func (a *Authorization) GetResponseMode() oidc.ResponseMode { return a.Request.ResponseMode }
func (a *Authorization) GetScopes() []string                { return a.Request.Scopes }
func (a *Authorization) GetState() string                   { return a.Request.State }
func (a *Authorization) GetSubject() string                 { return a.Subject }
func (a *Authorization) Done() bool                         { return a.Authenticated }

func (s *Store) CreateAuthRequest(ctx context.Context, r *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	c, err := s.Client(ctx, r.ClientID)
	if err != nil {
		return nil, err
	}
	if r.ResponseType != oidc.ResponseTypeCode || !slices.Contains(c.Redirects, r.RedirectURI) {
		return nil, oidc.ErrInvalidRequest().WithDescription("code flow and exact registered redirect URI required")
	}
	if r.CodeChallengeMethod != oidc.CodeChallengeMethodS256 || len(r.CodeChallenge) != 43 || r.State == "" || r.Nonce == "" {
		return nil, oidc.ErrInvalidRequest().WithDescription("S256 PKCE, state and nonce required")
	}
	if !slices.Contains(r.Scopes, oidc.ScopeOpenID) {
		return nil, oidc.ErrInvalidScope()
	}
	for _, scope := range r.Scopes {
		if !slices.Contains(c.Scopes, scope) {
			return nil, oidc.ErrInvalidScope()
		}
	}
	// Never display UI in a prompt=none request. Silent consent is not implemented.
	if slices.Contains(r.Prompt, oidc.PromptNone) {
		return nil, oidc.ErrLoginRequired()
	}
	a := &Authorization{ID: RandomToken(), CreatedAt: time.Now().UTC(), Request: *r}
	data, err := marshal(a)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO auth_requests(id,client_id,payload,expires_at) VALUES($1,$2,$3,now()+interval '5 minutes')`, a.ID, c.ID, data)
	return a, err
}
func (s *Store) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	var data []byte
	err := s.DB.QueryRow(ctx, `SELECT payload FROM auth_requests WHERE id=$1 AND expires_at>now() AND consumed_at IS NULL`, id).Scan(&data)
	if err != nil {
		return nil, ErrExpired
	}
	var a Authorization
	err = json.Unmarshal(data, &a)
	return &a, err
}
func (s *Store) CompleteAuthorization(ctx context.Context, id string, session *BrowserSession) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var data []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM auth_requests WHERE id=$1 AND expires_at>now() AND consumed_at IS NULL AND code_hash IS NULL FOR UPDATE`, id).Scan(&data)
	if err != nil {
		return ErrExpired
	}
	var a Authorization
	if err = json.Unmarshal(data, &a); err != nil {
		return err
	}
	if a.Authenticated {
		return ErrExpired
	}
	if _, err = s.Identity(ctx, session.IdentityID); err != nil {
		return err
	}
	a.Subject = session.IdentityID
	a.SessionID = session.ID
	a.AuthTime = session.AuthenticatedAt
	a.Authenticated = true
	data, err = marshal(a)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_requests SET payload=$2 WHERE id=$1`, id, data); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE link_intents SET subject=$3,state='confirmed' WHERE client_id=$1 AND nonce=$2 AND state='pending' AND expires_at>now()`, a.Request.ClientID, a.Request.Nonce, a.Subject); err != nil {
		return err
	}
	if err = audit(ctx, tx, a.Subject, "oidc.authorize", a.Request.ClientID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) SaveAuthCode(ctx context.Context, id, code string) error {
	result, err := s.DB.Exec(ctx, `UPDATE auth_requests SET code_hash=$2,code_expires_at=now()+interval '60 seconds' WHERE id=$1 AND code_hash IS NULL AND consumed_at IS NULL AND expires_at>now() AND (payload->>'Authenticated')::boolean`, id, Hash(code))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrExpired
	}
	return nil
}
func (s *Store) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	// Consume atomically before returning. Even invalid exchanges burn the code;
	// two concurrent token requests can never both mint tokens from one code.
	var data []byte
	err := s.DB.QueryRow(ctx, `UPDATE auth_requests SET consumed_at=now() WHERE code_hash=$1 AND consumed_at IS NULL AND code_expires_at>now() AND expires_at>now() RETURNING payload`, Hash(code)).Scan(&data)
	if err != nil {
		return nil, oidc.ErrInvalidGrant().WithDescription("authorization code expired or consumed")
	}
	var a Authorization
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	if !a.Authenticated {
		return nil, errors.New("authorization not completed")
	}
	return &a, nil
}
func (s *Store) DeleteAuthRequest(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `UPDATE auth_requests SET consumed_at=COALESCE(consumed_at,now()) WHERE id=$1`, id)
	return err
}
