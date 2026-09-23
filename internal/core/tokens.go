package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type Token struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_id"`
	Subject       string    `json:"sub"`
	Audience      []string  `json:"aud"`
	Scopes        []string  `json:"scope"`
	AuthTime      time.Time `json:"auth_time"`
	SessionID     string    `json:"sid,omitempty"`
	FamilyID      string    `json:"family_id,omitempty"`
	Service       bool      `json:"service"`
	Grant         string    `json:"grant,omitempty"`
	Delegated     bool      `json:"delegated,omitempty"`
	SourceTokenID string    `json:"source_token_id,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// A service request must not implement op.RefreshTokenRequest: the protocol
// library uses that interface to decide whether to issue a refresh token.
type ServiceRequest struct{ Token *Token }

func (t *ServiceRequest) GetSubject() string    { return t.Token.Subject }
func (t *ServiceRequest) GetAudience() []string { return t.Token.Audience }
func (t *ServiceRequest) GetScopes() []string   { return t.Token.Scopes }

func (t *Token) GetSubject() string     { return t.Subject }
func (t *Token) GetAudience() []string  { return t.Audience }
func (t *Token) GetScopes() []string    { return t.Scopes }
func (t *Token) GetClientID() string    { return t.ClientID }
func (t *Token) GetAuthTime() time.Time { return t.AuthTime }
func (t *Token) GetAMR() []string {
	if t.Service {
		return nil
	}
	return []string{"pwd"}
}
func (t *Token) SetCurrentScopes(scopes []string) { t.Scopes = scopes }

func tokenRequest(r op.TokenRequest) (*Token, error) {
	t := &Token{ID: RandomToken(), Subject: r.GetSubject(), Audience: r.GetAudience(), Scopes: r.GetScopes(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	switch v := r.(type) {
	case *ServiceRequest:
		t.ClientID = v.Token.ClientID
		t.Service = true
	case *Authorization:
		t.ClientID = v.GetClientID()
		t.AuthTime = v.AuthTime
		t.SessionID = v.SessionID
	case *Token:
		t.ClientID = v.ClientID
		t.AuthTime = v.AuthTime
		t.SessionID = v.SessionID
		t.Service = v.Service
		t.FamilyID = v.FamilyID
	case *op.DeviceAuthorizationState:
		t.ClientID = v.ClientID
		t.AuthTime = v.AuthTime
		t.Grant = "device_code"
	case op.TokenExchangeRequest:
		t.ClientID = v.GetClientID()
		t.AuthTime = v.GetAuthTime()
		t.Audience = v.GetAudience()
		t.Delegated = true
	default:
		return nil, errors.New("unsupported token request")
	}
	return t, nil
}

func (s *Store) validateTokenSubject(ctx context.Context, t *Token) error {
	if _, err := s.Client(ctx, t.ClientID); err != nil {
		return ErrUnauthorized
	}
	if t.Service {
		return nil
	}
	if _, err := s.Identity(ctx, t.Subject); err != nil {
		return ErrUnauthorized
	}
	if t.SessionID != "" {
		var active bool
		err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM browser_sessions WHERE id=$1 AND identity_id=$2 AND revoked_at IS NULL AND expires_at>now())`, t.SessionID, t.Subject).Scan(&active)
		if err != nil {
			return err
		}
		if !active {
			return ErrUnauthorized
		}
	}
	return nil
}

func insertAccess(ctx context.Context, tx pgx.Tx, t *Token) error {
	data, err := marshal(t)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_tokens(id,client_id,subject,family_id,payload,expires_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6)`, t.ID, t.ClientID, t.Subject, t.FamilyID, data, t.ExpiresAt)
	return err
}
func (s *Store) CreateAccessToken(ctx context.Context, r op.TokenRequest) (string, time.Time, error) {
	t, err := tokenRequest(r)
	if err != nil {
		return "", time.Time{}, err
	}
	if err = s.validateTokenSubject(ctx, t); err != nil {
		return "", time.Time{}, err
	}
	if exchange, ok := r.(op.TokenExchangeRequest); ok {
		source, sourceErr := s.ActiveToken(ctx, exchange.GetExchangeSubjectTokenIDOrToken())
		if sourceErr != nil || source.ClientID != t.ClientID || source.Subject != t.Subject || source.Delegated || source.Service {
			return "", time.Time{}, ErrForbidden
		}
		t.SessionID = source.SessionID
		t.SourceTokenID = source.ID
		t.AuthTime = source.AuthTime
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if err = insertAccess(ctx, tx, t); err != nil {
		return "", time.Time{}, err
	}
	err = tx.Commit(ctx)
	return t.ID, t.ExpiresAt, err
}
func (s *Store) CreateAccessAndRefreshTokens(ctx context.Context, r op.TokenRequest, current string) (string, string, time.Time, error) {
	t, err := tokenRequest(r)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if t.Service {
		return "", "", time.Time{}, ErrForbidden
	}
	if err = s.validateTokenSubject(ctx, t); err != nil {
		return "", "", time.Time{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	var expires time.Time
	if current == "" {
		t.FamilyID = RandomToken()
		expires = time.Now().UTC().Add(7 * 24 * time.Hour)
		_, err = tx.Exec(ctx, `INSERT INTO token_families(id,subject,client_id,session_id,expires_at) VALUES($1,$2,$3,$4,$5)`, t.FamilyID, t.Subject, t.ClientID, t.SessionID, expires)
		if err != nil {
			return "", "", time.Time{}, err
		}
	} else {
		var used, revoked *time.Time
		var subject, clientID string
		err = tx.QueryRow(ctx, `SELECT f.id,f.subject,f.client_id,f.expires_at,f.revoked_at,r.consumed_at FROM refresh_tokens r JOIN token_families f ON f.id=r.family_id WHERE r.token_hash=$1 FOR UPDATE OF f,r`, Hash(current)).Scan(&t.FamilyID, &subject, &clientID, &expires, &revoked, &used)
		if err != nil || subject != t.Subject || clientID != t.ClientID {
			return "", "", time.Time{}, op.ErrInvalidRefreshToken
		}
		if used != nil {
			if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, t.FamilyID); err != nil {
				return "", "", time.Time{}, err
			}
			if err = audit(ctx, tx, t.Subject, "refresh.replay", t.FamilyID); err != nil {
				return "", "", time.Time{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return "", "", time.Time{}, err
			}
			return "", "", time.Time{}, op.ErrInvalidRefreshToken
		}
		if revoked != nil || expires.Before(time.Now()) {
			return "", "", time.Time{}, op.ErrInvalidRefreshToken
		}
		if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET consumed_at=now() WHERE token_hash=$1`, Hash(current)); err != nil {
			return "", "", time.Time{}, err
		}
	}
	raw := RandomToken()
	data, err := marshal(t)
	if err != nil {
		return "", "", time.Time{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO refresh_tokens(token_hash,family_id,payload,expires_at) VALUES($1,$2,$3,$4)`, Hash(raw), t.FamilyID, data, expires)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if err = insertAccess(ctx, tx, t); err != nil {
		return "", "", time.Time{}, err
	}
	err = tx.Commit(ctx)
	return t.ID, raw, t.ExpiresAt, err
}

func (s *Store) TokenRequestByRefreshToken(ctx context.Context, raw string) (op.RefreshTokenRequest, error) {
	var data []byte
	var used, revoked *time.Time
	var family string
	err := s.DB.QueryRow(ctx, `SELECT r.payload,r.consumed_at,f.revoked_at,f.id FROM refresh_tokens r JOIN token_families f ON f.id=r.family_id WHERE r.token_hash=$1 AND r.expires_at>now() AND f.expires_at>now()`, Hash(raw)).Scan(&data, &used, &revoked, &family)
	if err != nil || revoked != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	if used != nil {
		_, err = s.DB.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, family)
		if err != nil {
			return nil, err
		}
		return nil, op.ErrInvalidRefreshToken
	}
	var t Token
	if err = json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if err = s.validateTokenSubject(ctx, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ActiveToken(ctx context.Context, id string) (*Token, error) {
	var data []byte
	err := s.DB.QueryRow(ctx, `SELECT a.payload FROM access_tokens a JOIN applications client ON client.id=a.client_id AND client.active LEFT JOIN token_families f ON f.id=a.family_id WHERE a.id=$1 AND a.revoked_at IS NULL AND a.expires_at>now() AND (a.family_id IS NULL OR (f.revoked_at IS NULL AND f.expires_at>now()))`, id).Scan(&data)
	if err != nil {
		return nil, ErrUnauthorized
	}
	var t Token
	if err = json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if err = s.validateTokenSubject(ctx, &t); err != nil {
		return nil, err
	}
	if t.Delegated {
		if t.SourceTokenID == "" || t.SourceTokenID == t.ID {
			return nil, ErrUnauthorized
		}
		parent, parentErr := s.ActiveToken(ctx, t.SourceTokenID)
		if parentErr != nil || parent.Delegated || parent.Service || parent.ClientID != t.ClientID || parent.Subject != t.Subject {
			return nil, ErrUnauthorized
		}
	}
	return &t, nil
}
func (s *Store) GetRefreshTokenInfo(ctx context.Context, client, raw string) (string, string, error) {
	var subject string
	err := s.DB.QueryRow(ctx, `SELECT f.subject FROM refresh_tokens r JOIN token_families f ON f.id=r.family_id WHERE r.token_hash=$1 AND f.client_id=$2`, Hash(raw), client).Scan(&subject)
	if err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	return subject, raw, nil
}
func (s *Store) RevokeToken(ctx context.Context, raw, subject, client string) *oidc.Error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return oidc.ErrServerError()
	}
	defer tx.Rollback(ctx)
	// Revocation is idempotent and client-scoped, regardless of token representation.
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE client_id=$1 AND id IN (SELECT family_id FROM refresh_tokens WHERE token_hash=$2)`, client, Hash(raw)); err != nil {
		return oidc.ErrServerError()
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND client_id=$2`, raw, client); err != nil {
		return oidc.ErrServerError()
	}
	if err = tx.Commit(ctx); err != nil {
		return oidc.ErrServerError()
	}
	return nil
}
func (s *Store) TerminateSession(ctx context.Context, subject, client string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND client_id=$2`, subject, client); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND client_id=$2`, subject, client); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ClientCredentials(ctx context.Context, id, secret string) (op.Client, error) {
	if err := s.AuthorizeClientIDSecret(ctx, id, secret); err != nil {
		return nil, err
	}
	return s.Client(ctx, id)
}
func (s *Store) ClientCredentialsTokenRequest(ctx context.Context, id string, scopes []string) (op.TokenRequest, error) {
	c, err := s.Client(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Public || !slices.Contains(c.Grants, oidc.GrantTypeClientCredentials) {
		return nil, ErrForbidden
	}
	for _, scope := range scopes {
		if !slices.Contains(c.Scopes, scope) || scope == oidc.ScopeOpenID || scope == oidc.ScopeOfflineAccess {
			return nil, oidc.ErrInvalidScope()
		}
	}
	if len(c.Resources) == 0 {
		return nil, ErrForbidden
	}
	return &ServiceRequest{Token: &Token{ClientID: id, Subject: id, Service: true, Scopes: scopes, Audience: c.Resources}}, nil
}
func (s *Store) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}
func (s *Store) userInfo(ctx context.Context, info *oidc.UserInfo, subject string, scopes []string) error {
	u, err := s.Identity(ctx, subject)
	if err != nil {
		return err
	}
	info.Subject = u.ID
	if slices.Contains(scopes, oidc.ScopeProfile) {
		info.Name = u.Name
	}
	if slices.Contains(scopes, oidc.ScopeEmail) {
		info.Email = u.Email
		info.EmailVerified = oidc.Bool(u.EmailVerified)
	}
	return nil
}
func (s *Store) SetUserinfoFromRequest(ctx context.Context, info *oidc.UserInfo, r op.IDTokenRequest, scopes []string) error {
	if err := s.userInfo(ctx, info, r.GetSubject(), scopes); err != nil {
		return err
	}
	switch v := r.(type) {
	case *Authorization:
		info.AppendClaims("sid", v.SessionID)
	case *Token:
		info.AppendClaims("sid", v.SessionID)
	}
	return nil
}
func (s *Store) SetUserinfoFromToken(ctx context.Context, info *oidc.UserInfo, id, subject, origin string) error {
	t, err := s.ActiveToken(ctx, id)
	if err != nil {
		return err
	}
	if t.Subject != subject || t.Service {
		return ErrForbidden
	}
	// UserInfo is server-to-server; browser cross-origin access is not enabled.
	if origin != "" {
		return ErrForbidden
	}
	return s.userInfo(ctx, info, t.Subject, t.Scopes)
}
func (s *Store) SetIntrospectionFromToken(ctx context.Context, info *oidc.IntrospectionResponse, id, subject, client string) error {
	t, err := s.ActiveToken(ctx, id)
	if err != nil {
		return err
	}
	if t.Subject != subject {
		return ErrForbidden
	}
	if !slices.Contains(t.Audience, client) && t.ClientID != client {
		if !t.Delegated {
			return ErrForbidden
		}
		var policies int
		if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM exchange_policies WHERE caller_client=$1 AND target_client=$2 AND resource=ANY($3) AND scope=ANY($4) AND active`, t.ClientID, client, t.Audience, t.Scopes).Scan(&policies); err != nil || policies != len(t.Scopes) {
			return ErrForbidden
		}
	}
	info.Subject = t.Subject
	info.ClientID = t.ClientID
	info.Scope = t.Scopes
	info.Audience = t.Audience
	info.Expiration = oidc.FromTime(t.ExpiresAt)
	return nil
}
func (s *Store) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}
func (s *Store) GetPrivateClaimsFromRequest(ctx context.Context, r op.TokenRequest, scopes []string) (map[string]any, error) {
	t, err := tokenRequest(r)
	if err != nil {
		return nil, err
	}
	return map[string]any{"token_use": "access", "client_id": t.ClientID, "scope": oidc.SpaceDelimitedArray(scopes), "sid": t.SessionID, "service": t.Service}, nil
}
func (s *Store) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, ErrForbidden
}
func (s *Store) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, ErrForbidden
}

var _ op.Storage = (*Store)(nil)
var _ op.ClientCredentialsStorage = (*Store)(nil)
