package core

import (
	"context"
	"slices"
	"strings"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// The exchange grant deliberately supports only a single target resource,
// short-lived access tokens, a first-party user token and explicit consent.
func (s *Store) ValidateTokenExchangeRequest(ctx context.Context, r op.TokenExchangeRequest) error {
	if r.GetExchangeSubjectTokenType() != oidc.AccessTokenType || r.GetExchangeActorTokenIDOrToken() != "" ||
		len(r.GetResourses()) != 1 || len(r.GetAudience()) != 1 || r.GetAudience()[0] != r.GetResourses()[0] || len(r.GetScopes()) == 0 ||
		(r.GetRequestedTokenType() != "" && r.GetRequestedTokenType() != oidc.AccessTokenType) {
		return oidc.ErrInvalidRequest().WithDescription("only scoped user access-token exchange is supported")
	}
	r.SetRequestedTokenType(oidc.AccessTokenType)
	caller, err := s.Client(ctx, r.GetClientID())
	if err != nil || caller.Public || !slices.Contains(caller.Grants, oidc.GrantTypeTokenExchange) || !slices.Contains(caller.Resources, r.GetResourses()[0]) {
		return oidc.ErrUnauthorizedClient()
	}
	source, err := s.ActiveToken(ctx, r.GetExchangeSubjectTokenIDOrToken())
	if err != nil || source.Subject != r.GetExchangeSubject() || source.Subject != r.GetSubject() || source.ClientID != caller.ID || source.Service || source.Delegated {
		return oidc.ErrInvalidGrant()
	}
	for _, scope := range r.GetScopes() {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") || scope == oidc.ScopeOpenID || scope == oidc.ScopeOfflineAccess || !slices.Contains(caller.Scopes, scope) || !slices.Contains(source.Scopes, scope) {
			return oidc.ErrInvalidScope()
		}
	}
	var allowed int
	err = s.DB.QueryRow(ctx, `SELECT count(*) FROM exchange_policies p JOIN exchange_consents c
	 ON c.caller_client=p.caller_client AND c.target_client=p.target_client AND c.resource=p.resource AND c.scope=p.scope
	 WHERE p.caller_client=$1 AND p.resource=$2 AND c.subject=$3 AND p.active AND c.revoked_at IS NULL
	 AND EXISTS(SELECT 1 FROM applications a WHERE a.id=p.target_client AND a.active)
	 AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=p.caller_client AND l.subject=$3 AND l.state='active')
	 AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=p.target_client AND l.subject=$3 AND l.state='active')
	 AND p.scope=ANY($4)`, caller.ID, r.GetResourses()[0], source.Subject, r.GetScopes()).Scan(&allowed)
	if err != nil {
		return err
	}
	if allowed != len(r.GetScopes()) {
		return oidc.ErrAccessDenied()
	}
	return nil
}

func (s *Store) CreateTokenExchangeRequest(ctx context.Context, r op.TokenExchangeRequest) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = audit(ctx, tx, r.GetSubject(), "token.exchange", r.GetClientID()+":"+r.GetResourses()[0]); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetPrivateClaimsFromTokenExchangeRequest(ctx context.Context, r op.TokenExchangeRequest) (map[string]any, error) {
	source, err := s.ActiveToken(ctx, r.GetExchangeSubjectTokenIDOrToken())
	if err != nil || source.ClientID != r.GetClientID() || source.Subject != r.GetSubject() {
		return nil, ErrForbidden
	}
	return map[string]any{"token_use": "access", "client_id": r.GetClientID(), "scope": oidc.SpaceDelimitedArray(r.GetScopes()),
		"sid": source.SessionID, "service": false, "act": map[string]string{"sub": r.GetClientID()}, "delegated": true}, nil
}

func (s *Store) SetUserinfoFromTokenExchangeRequest(context.Context, *oidc.UserInfo, op.TokenExchangeRequest) error {
	return ErrForbidden
}

func (s *Store) SetExchangePolicy(ctx context.Context, actor, caller, target, resource, scope string, active bool) error {
	if caller == "" || target == "" || caller == target || resource == "" || scope == "" || strings.ContainsAny(scope, " \t\r\n") {
		return ErrForbidden
	}
	if active {
		client, err := s.Client(ctx, caller)
		if err != nil || client.Public || !slices.Contains(client.Grants, oidc.GrantTypeTokenExchange) || !slices.Contains(client.Resources, resource) || !slices.Contains(client.Scopes, scope) {
			return ErrForbidden
		}
		targetClient, err := s.Client(ctx, target)
		if err != nil || !slices.Contains(targetClient.Scopes, scope) {
			return ErrForbidden
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO exchange_policies(caller_client,target_client,resource,scope,active) VALUES($1,$2,$3,$4,$5)
	 ON CONFLICT(caller_client,target_client,resource,scope) DO UPDATE SET active=excluded.active`, caller, target, resource, scope, active)
	if err != nil {
		return err
	}
	if !active {
		if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE client_id=$1 AND payload->>'delegated'='true' AND payload->'aud' ? $2`, caller, resource); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, actor, "exchange.policy", caller+":"+target+":"+resource+":"+scope); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetExchangeConsent(ctx context.Context, subject, caller, target, resource, scope string, active bool) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exchange_policies p WHERE p.caller_client=$1 AND p.target_client=$2 AND p.resource=$3 AND p.scope=$4 AND p.active
	 AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=$1 AND l.subject=$5 AND l.state='active')
	 AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=$2 AND l.subject=$5 AND l.state='active'))`, caller, target, resource, scope, subject).Scan(&valid)
	if err != nil {
		return err
	}
	if active && !valid {
		return ErrForbidden
	}
	if active {
		_, err = tx.Exec(ctx, `INSERT INTO exchange_consents(subject,caller_client,target_client,resource,scope) VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(subject,caller_client,target_client,resource,scope) DO UPDATE SET revoked_at=NULL,granted_at=now()`, subject, caller, target, resource, scope)
	} else {
		_, err = tx.Exec(ctx, `UPDATE exchange_consents SET revoked_at=now() WHERE subject=$1 AND caller_client=$2 AND target_client=$3 AND resource=$4 AND scope=$5`, subject, caller, target, resource, scope)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE subject=$1 AND client_id=$2 AND payload->>'delegated'='true' AND payload->'aud' ? $3`, subject, caller, resource)
		}
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "exchange.consent", caller+":"+target+":"+resource+":"+scope); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ op.TokenExchangeStorage = (*Store)(nil)

type ExchangeOption struct {
	Caller    string `json:"caller_client"`
	Target    string `json:"target_client"`
	Resource  string `json:"resource"`
	Scope     string `json:"scope"`
	Active    bool   `json:"active"`
	Consented bool   `json:"consented,omitempty"`
}

func (s *Store) ExchangePolicies(ctx context.Context) ([]ExchangeOption, error) {
	rows, err := s.DB.Query(ctx, `SELECT caller_client,target_client,resource,scope,active FROM exchange_policies ORDER BY caller_client,target_client,resource,scope LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExchangeOption{}
	for rows.Next() {
		var item ExchangeOption
		if err = rows.Scan(&item.Caller, &item.Target, &item.Resource, &item.Scope, &item.Active); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ExchangeOptionsFor(ctx context.Context, subject string) ([]ExchangeOption, error) {
	rows, err := s.DB.Query(ctx, `SELECT p.caller_client,p.target_client,p.resource,p.scope,p.active,(c.revoked_at IS NULL AND c.subject IS NOT NULL)
	 FROM exchange_policies p LEFT JOIN exchange_consents c ON c.subject=$1 AND c.caller_client=p.caller_client AND c.target_client=p.target_client AND c.resource=p.resource AND c.scope=p.scope
	 WHERE p.active AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=p.caller_client AND l.subject=$1 AND l.state='active')
	 AND EXISTS(SELECT 1 FROM account_links l WHERE l.client_id=p.target_client AND l.subject=$1 AND l.state='active')
	 ORDER BY p.caller_client,p.target_client,p.resource,p.scope LIMIT 500`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExchangeOption{}
	for rows.Next() {
		var item ExchangeOption
		if err = rows.Scan(&item.Caller, &item.Target, &item.Resource, &item.Scope, &item.Active, &item.Consented); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
