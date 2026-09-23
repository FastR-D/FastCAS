package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type Client struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Redirects       []string         `json:"redirect_uris"`
	LogoutRedirects []string         `json:"post_logout_redirect_uris"`
	Scopes          []string         `json:"scopes"`
	Resources       []string         `json:"resources"`
	Grants          []oidc.GrantType `json:"grant_types"`
	Public          bool             `json:"public"`
	Development     bool             `json:"development"`
	BackchannelURL  string           `json:"backchannel_logout_uri,omitempty"`
	EventsURL       string           `json:"events_uri,omitempty"`
}

func (c *Client) GetID() string                    { return c.ID }
func (c *Client) RedirectURIs() []string           { return c.Redirects }
func (c *Client) PostLogoutRedirectURIs() []string { return c.LogoutRedirects }
func (c *Client) ApplicationType() op.ApplicationType {
	if c.Public {
		return op.ApplicationTypeUserAgent
	}
	return op.ApplicationTypeWeb
}
func (c *Client) AuthMethod() oidc.AuthMethod {
	if c.Public {
		return oidc.AuthMethodNone
	}
	return oidc.AuthMethodBasic
}
func (c *Client) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}
func (c *Client) GrantTypes() []oidc.GrantType        { return c.Grants }
func (c *Client) LoginURL(id string) string           { return "/login?auth_request_id=" + url.QueryEscape(id) }
func (c *Client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeJWT }
func (c *Client) IDTokenLifetime() time.Duration      { return 5 * time.Minute }
func (c *Client) DevMode() bool                       { return c.Development }
func (c *Client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func([]string) []string { return nil }
}
func (c *Client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(v []string) []string { return v }
}
func (c *Client) IsScopeAllowed(scope string) bool     { return slices.Contains(c.Scopes, scope) }
func (c *Client) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *Client) ClockSkew() time.Duration             { return 0 }

func validEndpoint(raw string, dev bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(u.Host, " \t\r\n;'\"\\") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return dev && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func validateCapabilities(c Client) error {
	if len(c.Grants) == 0 || len(c.Grants) > 5 || len(c.Scopes) > 64 || len(c.Resources) > 32 {
		return ErrForbidden
	}
	seen := map[string]bool{}
	for _, grant := range c.Grants {
		if grant != oidc.GrantTypeCode && grant != oidc.GrantTypeRefreshToken && grant != oidc.GrantTypeClientCredentials && grant != oidc.GrantTypeDeviceCode && grant != oidc.GrantTypeTokenExchange {
			return ErrForbidden
		}
		if seen[string(grant)] || (c.Public && (grant == oidc.GrantTypeClientCredentials || grant == oidc.GrantTypeTokenExchange)) || (!c.Public && grant == oidc.GrantTypeDeviceCode) {
			return ErrForbidden
		}
		seen[string(grant)] = true
	}
	if slices.Contains(c.Grants, oidc.GrantTypeCode) && len(c.Redirects) == 0 {
		return ErrForbidden
	}
	seen = map[string]bool{}
	for _, scope := range c.Scopes {
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n") || seen[scope] {
			return ErrForbidden
		}
		seen[scope] = true
	}
	seen = map[string]bool{}
	for _, resource := range c.Resources {
		if resource == "" || len(resource) > 128 || strings.ContainsAny(resource, " \t\r\n") || seen[resource] {
			return ErrForbidden
		}
		seen[resource] = true
	}
	if slices.Contains(c.Grants, oidc.GrantTypeTokenExchange) && (c.Public || len(c.Resources) == 0) {
		return ErrForbidden
	}
	if slices.Contains(c.Grants, oidc.GrantTypeClientCredentials) && (len(c.Resources) == 0 || len(c.Scopes) == 0) {
		return ErrForbidden
	}
	if serviceClient(c) && (c.Public || len(c.Redirects) != 0 || len(c.LogoutRedirects) != 0 || c.EventsURL != "" || c.BackchannelURL != "") {
		return ErrForbidden
	}
	if serviceClient(c) && (slices.Contains(c.Scopes, oidc.ScopeOpenID) || slices.Contains(c.Scopes, oidc.ScopeOfflineAccess)) {
		return ErrForbidden
	}
	return nil
}

func serviceClient(c Client) bool {
	return len(c.Grants) == 1 && c.Grants[0] == oidc.GrantTypeClientCredentials
}

func (s *Store) RegisterClient(ctx context.Context, c Client, secret string) error {
	return s.RegisterClientAs(ctx, "bootstrap", c, secret)
}
func (s *Store) RegisterClientAs(ctx context.Context, actor string, c Client, secret string) error {
	if c.ID == "" || len(c.ID) > 128 || c.Name == "" || len(c.Name) > 200 {
		return errors.New("client ID and name required")
	}
	if c.Development && !s.Dev {
		return errors.New("development client not allowed in production")
	}
	if !c.Public && len(secret) < 32 {
		return errors.New("client secret must contain at least 32 bytes")
	}
	if c.Public && secret != "" {
		return errors.New("public client cannot have a secret")
	}
	if len(c.Grants) == 0 {
		c.Grants = []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
	}
	if err := validateCapabilities(c); err != nil {
		return err
	}
	for _, grant := range c.Grants {
		if grant != oidc.GrantTypeCode && grant != oidc.GrantTypeRefreshToken && grant != oidc.GrantTypeClientCredentials && grant != oidc.GrantTypeDeviceCode && grant != oidc.GrantTypeTokenExchange {
			return errors.New("grant not enabled yet")
		}
		if c.Public && grant == oidc.GrantTypeClientCredentials {
			return ErrForbidden
		}
		if !c.Public && grant == oidc.GrantTypeDeviceCode {
			return ErrForbidden
		}
		if c.Public && grant == oidc.GrantTypeTokenExchange {
			return ErrForbidden
		}
	}
	if slices.Contains(c.Grants, oidc.GrantTypeCode) && len(c.Redirects) == 0 {
		return errors.New("redirect URI required")
	}
	urls := append(append([]string{}, c.Redirects...), c.LogoutRedirects...)
	if c.BackchannelURL != "" {
		urls = append(urls, c.BackchannelURL)
	}
	if c.EventsURL != "" {
		urls = append(urls, c.EventsURL)
	}
	for _, raw := range urls {
		if !validEndpoint(raw, c.Development) {
			return errors.New("invalid registered endpoint")
		}
	}
	data, err := marshal(c)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO applications(id,name,secret_hash,config) VALUES($1,$2,$3,$4)`, c.ID, c.Name, Hash(secret), data)
	if err != nil {
		return classify(err)
	}
	if err = audit(ctx, tx, actor, "application.create", c.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Client(ctx context.Context, id string) (*Client, error) {
	var data []byte
	err := s.DB.QueryRow(ctx, `SELECT config FROM applications WHERE id=$1 AND active`, id).Scan(&data)
	if err != nil {
		return nil, classify(err)
	}
	var c Client
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateClientCallbacksAs updates delivery endpoints without rotating or
// changing the client's authentication and authorization configuration.
func (s *Store) UpdateClientCallbacksAs(ctx context.Context, actor, id, eventsURL, backchannelURL string) (*Client, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var data []byte
	if err = tx.QueryRow(ctx, `SELECT config FROM applications WHERE id=$1 AND active FOR UPDATE`, id).Scan(&data); err != nil {
		return nil, classify(err)
	}
	var client Client
	if err = json.Unmarshal(data, &client); err != nil {
		return nil, err
	}
	if eventsURL != "" && !validEndpoint(eventsURL, client.Development) {
		return nil, errors.New("invalid events endpoint")
	}
	if backchannelURL != "" && !validEndpoint(backchannelURL, client.Development) {
		return nil, errors.New("invalid backchannel endpoint")
	}
	client.EventsURL, client.BackchannelURL = eventsURL, backchannelURL
	if serviceClient(client) && (eventsURL != "" || backchannelURL != "") {
		return nil, ErrForbidden
	}
	data, err = marshal(client)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE applications SET config=$2 WHERE id=$1`, id, data); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, actor, "application.callbacks.update", id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &client, nil
}

func (s *Store) UpdateClientCapabilitiesAs(ctx context.Context, actor, id string, grants []oidc.GrantType, scopes, resources []string) (*Client, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var data []byte
	if err = tx.QueryRow(ctx, `SELECT config FROM applications WHERE id=$1 AND active FOR UPDATE`, id).Scan(&data); err != nil {
		return nil, classify(err)
	}
	var client Client
	if err = json.Unmarshal(data, &client); err != nil {
		return nil, err
	}
	wasService := serviceClient(client)
	client.Grants, client.Scopes, client.Resources = grants, scopes, resources
	if wasService != serviceClient(client) {
		return nil, ErrForbidden
	}
	if err = validateCapabilities(client); err != nil {
		return nil, err
	}
	data, err = marshal(client)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE applications SET config=$2 WHERE id=$1`, id, data); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, actor, "application.capabilities.update", id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &client, nil
}
func (s *Store) GetClientByClientID(ctx context.Context, id string) (op.Client, error) {
	return s.Client(ctx, id)
}
func (s *Store) AuthorizeClientIDSecret(ctx context.Context, id, secret string) error {
	if secret == "" {
		return ErrUnauthorized
	}
	var current string
	var previous *string
	err := s.DB.QueryRow(ctx, `SELECT secret_hash,CASE WHEN previous_secret_expires_at>now() THEN previous_secret_hash ELSE NULL END
 FROM applications WHERE id=$1 AND active AND NOT (config->>'public')::boolean`, id).Scan(&current, &previous)
	if err != nil {
		return ErrUnauthorized
	}
	currentOK := equalHash(secret, current)
	previousOK := false
	if previous != nil {
		previousOK = equalHash(secret, *previous)
	}
	if !currentOK && !previousOK {
		return ErrUnauthorized
	}
	return nil
}

// RotateClientSecretAs replaces the current credential while retaining it for
// a bounded handover window. Rotation and audit either commit together or not at all.
func (s *Store) RotateClientSecretAs(ctx context.Context, actor, id string, grace time.Duration) (string, error) {
	if grace < 0 || grace > time.Hour {
		return "", ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var rawConfig []byte
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT config,secret_hash FROM applications WHERE id=$1 AND active FOR UPDATE`, id).Scan(&rawConfig, &oldHash)
	if err != nil {
		return "", classify(err)
	}
	var client Client
	if err = json.Unmarshal(rawConfig, &client); err != nil {
		return "", err
	}
	if client.Public {
		return "", ErrForbidden
	}
	secret := RandomToken()
	if _, err = tx.Exec(ctx, `UPDATE applications SET secret_hash=$2,previous_secret_hash=CASE WHEN $3::bigint>0 THEN $4 ELSE NULL END,
 previous_secret_expires_at=CASE WHEN $3::bigint>0 THEN now()+($3::bigint*interval '1 second') ELSE NULL END WHERE id=$1`, id, Hash(secret), int64(grace/time.Second), oldHash); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, actor, "application.secret.rotate", id); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return secret, nil
}
