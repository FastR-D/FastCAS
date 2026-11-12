package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type Authorization struct {
	ID               string
	CreatedAt        time.Time
	Request          oidc.AuthRequest
	Subject          string
	SessionID        string
	AuthTime         time.Time
	Authenticated    bool
	RequestedSubject string
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

// Identity scopes are trusted at application registration. Resource scopes
// require user approval once, and again if the application requests more.
func (s *Store) AuthorizationConsentRequired(ctx context.Context, a *Authorization, subject string) (bool, error) {
	var resources []string
	for _, scope := range a.Request.Scopes {
		if !slices.Contains([]string{"openid", "profile", "email", "offline_access"}, scope) {
			resources = append(resources, scope)
		}
	}
	if len(resources) == 0 {
		return false, nil
	}
	var approved bool
	err := s.DB.QueryRow(ctx, `SELECT scopes @> $3::text[] FROM authorization_consents WHERE subject=$1 AND client_id=$2`, subject, a.Request.ClientID, resources).Scan(&approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return !approved, err
}

func (a *Authorization) SessionFresh(session *BrowserSession) bool {
	if session == nil || !session.ExpiresAt.After(time.Now()) {
		return false
	}
	for _, prompt := range a.Request.Prompt {
		if prompt == oidc.PromptLogin && session.AuthenticatedAt.Before(a.CreatedAt) {
			return false
		}
	}
	if a.Request.MaxAge == nil {
		return true
	}
	// max_age=0 requests authentication for this transaction, rather than an
	// impossible zero-duration session after the credential POST.
	if *a.Request.MaxAge == 0 {
		return !session.AuthenticatedAt.Before(a.CreatedAt)
	}
	return time.Since(session.AuthenticatedAt) <= time.Duration(*a.Request.MaxAge)*time.Second
}
func (s *Store) CreateAuthRequest(ctx context.Context, r *oidc.AuthRequest, requestedSubject string) (op.AuthRequest, error) {
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
	a := &Authorization{ID: RandomToken(), CreatedAt: time.Now().UTC(), Request: *r, RequestedSubject: requestedSubject}
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
	if session == nil {
		return ErrUnauthorized
	}
	// Match the identity -> session -> authorization lock order used by account
	// recovery. Revocation cannot race completion and lock ordering stays stable.
	var active string
	if err = tx.QueryRow(ctx, `SELECT id FROM identities WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR SHARE`, session.IdentityID).Scan(&active); err != nil {
		return classify(err)
	}
	var current BrowserSession
	err = tx.QueryRow(ctx, `SELECT id,identity_id,authenticated_at,expires_at FROM browser_sessions
 WHERE id=$1 AND identity_id=$2 AND revoked_at IS NULL AND expires_at>now() FOR SHARE`, session.ID, session.IdentityID).
		Scan(&current.ID, &current.IdentityID, &current.AuthenticatedAt, &current.ExpiresAt)
	if err != nil {
		return classify(err)
	}
	session = &current
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
	if !a.SessionFresh(session) {
		return ErrUnauthorized
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
	if _, err = tx.Exec(ctx, `INSERT INTO authorization_consents(subject,client_id,scopes) VALUES($1,$2,$3)
 ON CONFLICT(subject,client_id) DO UPDATE SET scopes=ARRAY(SELECT DISTINCT unnest(authorization_consents.scopes || EXCLUDED.scopes)),updated_at=now()`, a.Subject, a.Request.ClientID, []string(a.Request.Scopes)); err != nil {
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
