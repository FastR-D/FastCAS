// Package fastcas adds optional OIDC login and explicit account linking. It never
// creates local users, changes their passwords, or installs a global auth gate.
package fastcas

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURI string
	AllowLoopbackHTTP                           bool
	HTTPClient                                  *http.Client
}
type Transaction struct {
	State           string      `json:"state"`
	Nonce           string      `json:"nonce"`
	Verifier        string      `json:"verifier"`
	BindingHash     string      `json:"binding_hash"`
	Purpose         string      `json:"purpose"`
	ReturnTo        string      `json:"return_to"`
	LocalAccountRef string      `json:"local_account_ref,omitempty"`
	LocalSessionID  string      `json:"local_session_id,omitempty"`
	ExpiresAt       time.Time   `json:"expires_at"`
	Intent          *LinkIntent `json:"intent,omitempty"`
}

// TransactionStore must persist transactions and atomically consume them in Take.
type TransactionStore interface {
	Put(context.Context, Transaction) error
	Take(context.Context, string) (*Transaction, error)
}
type ReplayStore interface {
	Accept(context.Context, string, time.Time) (bool, error)
}
type Client struct {
	config   Config
	store    TransactionStore
	http     *http.Client
	mu       sync.Mutex
	provider *oidc.Provider
	metadata Metadata
}

// ResetVerificationCache discards discovery and JWKS state after a trusted
// emergency key rotation. Drain in-flight authentication before calling it:
// requests that already hold a verifier can still use the old key set. This
// does not revoke application sessions previously issued by the caller.
func (c *Client) ResetVerificationCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provider = nil
	c.metadata = Metadata{}
}

type Diagnostics struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	Ready    bool   `json:"ready"`
}

// Diagnose checks provider discovery without exposing application credentials.
func (c *Client) Diagnose(ctx context.Context) (Diagnostics, error) {
	_, _, err := c.discovery(ctx)
	if err != nil {
		return Diagnostics{}, err
	}
	return Diagnostics{Issuer: c.config.Issuer, ClientID: c.config.ClientID, Ready: true}, nil
}

type Metadata struct {
	Issuer                      string `json:"issuer"`
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	JWKSURI                     string `json:"jwks_uri"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
	RevocationEndpoint          string `json:"revocation_endpoint"`
	IntrospectionEndpoint       string `json:"introspection_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}
type Identity struct {
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
	Name          string `json:"name,omitempty"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	SessionID     string `json:"sid,omitempty"`
}
type LinkIntent struct {
	ID        string    `json:"id"`
	ClientID  string    `json:"client_id"`
	LocalRef  string    `json:"local_account_ref"`
	Nonce     string    `json:"nonce"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Link struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	LocalRef   string    `json:"local_account_ref"`
	Subject    string    `json:"subject"`
	State      string    `json:"state"`
	Version    int64     `json:"version"`
	VerifiedAt time.Time `json:"verified_at"`
}
type BeginOptions struct {
	BrowserBinding, ReturnTo, LocalAccountRef, LocalSessionID string
	Scopes                                                    []string
}
type FinishOptions struct{ BrowserBinding, LocalAccountRef, LocalSessionID string }
type LoginResult struct {
	Identity    Identity
	Transaction Transaction
	Token       *oauth2.Token
	IDToken     string
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("FastCAS %s (%d): %s", e.Code, e.Status, e.Message)
}

func New(config Config, store TransactionStore) (*Client, error) {
	if store == nil {
		return nil, errors.New("persistent transaction store required")
	}
	issuer, err := endpoint(config.Issuer, config.AllowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	if issuer.Path != "" || issuer.RawQuery != "" || config.ClientID == "" {
		return nil, errors.New("issuer must be an origin; client ID required")
	}
	if _, err = endpoint(config.RedirectURI, config.AllowLoopbackHTTP); err != nil {
		return nil, err
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{config: config, store: store, http: client}, nil
}
func endpoint(raw string, dev bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("invalid endpoint URL")
	}
	if u.Scheme != "https" && !(dev && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, errors.New("HTTPS required outside explicit loopback development")
	}
	return u, nil
}
func RandomBinding() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (c *Client) ctx(ctx context.Context) context.Context { return oidc.ClientContext(ctx, c.http) }
func (c *Client) discovery(ctx context.Context) (*oidc.Provider, Metadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.provider, c.metadata, nil
	}
	p, err := oidc.NewProvider(c.ctx(ctx), c.config.Issuer)
	if err != nil {
		return nil, Metadata{}, err
	}
	var m Metadata
	if err = p.Claims(&m); err != nil {
		return nil, m, err
	}
	for _, raw := range []string{m.AuthorizationEndpoint, m.TokenEndpoint, m.JWKSURI, m.UserinfoEndpoint} {
		u, err := endpoint(raw, c.config.AllowLoopbackHTTP)
		if err != nil {
			return nil, m, err
		}
		if u.Scheme+"://"+u.Host != c.config.Issuer {
			return nil, m, errors.New("endpoint does not belong to configured issuer")
		}
	}
	c.provider, c.metadata = p, m
	return p, m, nil
}
func (c *Client) oauth(p *oidc.Provider, scopes []string) *oauth2.Config {
	ep := p.Endpoint()
	ep.AuthStyle = oauth2.AuthStyleInHeader
	return &oauth2.Config{ClientID: c.config.ClientID, ClientSecret: c.config.ClientSecret, RedirectURL: c.config.RedirectURI, Endpoint: ep, Scopes: scopes}
}
func (c *Client) BeginLogin(ctx context.Context, options BeginOptions) (string, error) {
	return c.begin(ctx, "login", options)
}
func (c *Client) BeginLink(ctx context.Context, options BeginOptions) (string, error) {
	if options.LocalAccountRef == "" || options.LocalSessionID == "" {
		return "", errors.New("recent local account and session proof required")
	}
	return c.begin(ctx, "link", options)
}

// BeginRegistration starts explicit signup for a newly reserved local account.
// The application must enforce its signup policy and atomically reject an
// existing local ID when committing the new ordinary user and pending link.
func (c *Client) BeginRegistration(ctx context.Context, newLocalAccountRef string, options BeginOptions) (string, error) {
	if newLocalAccountRef == "" {
		return "", errors.New("new local account reference required")
	}
	options.LocalAccountRef, options.LocalSessionID = newLocalAccountRef, ""
	return c.begin(ctx, "register", options)
}
func (c *Client) begin(ctx context.Context, purpose string, options BeginOptions) (string, error) {
	if len(options.BrowserBinding) < 32 {
		return "", errors.New("fresh browser binding required")
	}
	path := options.ReturnTo
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") {
		return "", errors.New("application-relative return path required")
	}
	p, _, err := c.discovery(ctx)
	if err != nil {
		return "", err
	}
	scopes := options.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	t := Transaction{State: RandomBinding(), Nonce: RandomBinding(), Verifier: oauth2.GenerateVerifier(), BindingHash: hash(options.BrowserBinding), Purpose: purpose, ReturnTo: path, ExpiresAt: time.Now().Add(5 * time.Minute)}
	if purpose == "link" || purpose == "register" {
		t.LocalAccountRef, t.LocalSessionID = options.LocalAccountRef, options.LocalSessionID
		var intent LinkIntent
		if err = c.api(ctx, "POST", "/api/v1/link-intents", map[string]string{"local_account_ref": options.LocalAccountRef}, map[string]string{"Idempotency-Key": t.State}, &intent); err != nil {
			return "", err
		}
		t.Intent = &intent
		t.Nonce = intent.Nonce
	}
	if err = c.store.Put(ctx, t); err != nil {
		return "", err
	}
	return c.oauth(p, scopes).AuthCodeURL(t.State, oauth2.S256ChallengeOption(t.Verifier), oidc.Nonce(t.Nonce)), nil
}
func (c *Client) FinishLogin(ctx context.Context, callback string, options FinishOptions) (*LoginResult, error) {
	u, err := url.Parse(callback)
	if err != nil {
		return nil, err
	}
	expected, _ := url.Parse(c.config.RedirectURI)
	if u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path {
		return nil, errors.New("callback URL mismatch")
	}
	state := u.Query().Get("state")
	if state == "" {
		return nil, errors.New("callback state required")
	}
	t, err := c.store.Take(ctx, state)
	if err != nil {
		return nil, err
	}
	if t == nil || time.Now().After(t.ExpiresAt) || subtle.ConstantTimeCompare([]byte(t.BindingHash), []byte(hash(options.BrowserBinding))) != 1 {
		return nil, errors.New("transaction expired, consumed or belongs to another browser")
	}
	if t.Purpose == "link" && (options.LocalAccountRef != t.LocalAccountRef || options.LocalSessionID != t.LocalSessionID) {
		return nil, errors.New("local account changed during linking")
	}
	if u.Query().Get("error") != "" || u.Query().Get("code") == "" {
		return nil, errors.New("authorization was not granted")
	}
	p, _, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	token, err := c.oauth(p, nil).Exchange(c.ctx(ctx), u.Query().Get("code"), oauth2.VerifierOption(t.Verifier))
	if err != nil {
		return nil, errors.New("code exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("ID token required")
	}
	idToken, err := p.Verifier(&oidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: []string{"RS256"}}).Verify(c.ctx(ctx), raw)
	if err != nil {
		return nil, err
	}
	if idToken.Nonce != t.Nonce {
		return nil, errors.New("nonce mismatch")
	}
	profile, err := p.UserInfo(c.ctx(ctx), oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, err
	}
	if profile.Subject != idToken.Subject {
		return nil, errors.New("userinfo subject mismatch")
	}
	var claims struct {
		Name string `json:"name"`
		SID  string `json:"sid"`
	}
	if err = profile.Claims(&claims); err != nil {
		return nil, err
	}
	name := claims.Name
	if err = idToken.Claims(&claims); err != nil {
		return nil, err
	}
	return &LoginResult{Identity: Identity{Issuer: c.config.Issuer, Subject: idToken.Subject, Name: name, Email: profile.Email, EmailVerified: profile.EmailVerified, SessionID: claims.SID}, Transaction: *t, Token: token, IDToken: raw}, nil
}
func (c *Client) PrepareLink(ctx context.Context, result *LoginResult) (*Link, error) {
	t := result.Transaction
	if (t.Purpose != "link" && t.Purpose != "register") || t.Intent == nil {
		return nil, errors.New("completed link transaction required")
	}
	var l Link
	err := c.api(ctx, "POST", "/api/v1/link-intents/"+url.PathEscape(t.Intent.ID)+"/prepare", map[string]string{"nonce": t.Intent.Nonce, "subject": result.Identity.Subject}, nil, &l)
	return &l, err
}
func (c *Client) ActivateLink(ctx context.Context, id string) (*Link, error) {
	var l Link
	err := c.api(ctx, "POST", "/api/v1/link-intents/"+url.PathEscape(id)+"/activate", nil, nil, &l)
	return &l, err
}
func (c *Client) GetLink(ctx context.Context, id string) (*Link, error) {
	var l Link
	err := c.api(ctx, "GET", "/api/v1/account-links/"+url.PathEscape(id), nil, nil, &l)
	return &l, err
}
func (c *Client) ResolveLink(ctx context.Context, subject string) (*Link, error) {
	var l Link
	err := c.api(ctx, "GET", "/api/v1/account-links/resolve?subject="+url.QueryEscape(subject), nil, nil, &l)
	return &l, err
}
func (c *Client) RevokeLink(ctx context.Context, link *Link) (*Link, error) {
	var l Link
	err := c.api(ctx, "POST", "/api/v1/account-links/"+url.PathEscape(link.ID)+"/revoke", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, link.Version)}, &l)
	return &l, err
}
func (c *Client) Refresh(ctx context.Context, token *oauth2.Token) (*oauth2.Token, error) {
	p, _, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	copy := *token
	copy.AccessToken = ""
	copy.Expiry = time.Now().Add(-time.Minute)
	return c.oauth(p, nil).TokenSource(c.ctx(ctx), &copy).Token()
}

type AccessClaims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  []string `json:"aud"`
	ClientID  string   `json:"client_id"`
	Scope     string   `json:"scope"`
	TokenUse  string   `json:"token_use"`
	Service   bool     `json:"service"`
	SessionID string   `json:"sid"`
}

func (c *Client) VerifyAccessToken(ctx context.Context, raw, audience string, scopes ...string) (*AccessClaims, error) {
	p, _, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	verified, err := p.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{"RS256"}}).Verify(c.ctx(ctx), raw)
	if err != nil {
		return nil, err
	}
	var claims AccessClaims
	if err = verified.Claims(&claims); err != nil {
		return nil, err
	}
	if claims.TokenUse != "access" || claims.Subject == "" {
		return nil, errors.New("API access token required")
	}
	available := map[string]bool{}
	for _, scope := range strings.Fields(claims.Scope) {
		available[scope] = true
	}
	for _, scope := range scopes {
		if !available[scope] {
			return nil, errors.New("insufficient scope")
		}
	}
	return &claims, nil
}
func (c *Client) api(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	if c.config.ClientSecret == "" {
		return errors.New("application server credential required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.config.Issuer+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	secureClient := *c.http
	secureClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := secureClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data := io.LimitReader(response.Body, 1<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		e := &APIError{Code: "request_failed", Message: "request failed", Status: response.StatusCode}
		_ = json.NewDecoder(data).Decode(e)
		return e
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(data).Decode(out)
}
