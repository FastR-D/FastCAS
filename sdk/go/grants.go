package fastcas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenResponse struct {
	AccessToken     string    `json:"access_token"`
	TokenType       string    `json:"token_type"`
	IssuedTokenType string    `json:"issued_token_type,omitempty"`
	Scope           string    `json:"scope,omitempty"`
	ExpiresIn       int64     `json:"expires_in"`
	Expiry          time.Time `json:"-"`
}

func (c *Client) tokenGrant(ctx context.Context, values url.Values) (*TokenResponse, error) {
	if c.config.ClientSecret == "" {
		return nil, errors.New("confidential client credential required")
	}
	_, metadata, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != 200 {
		var problem struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = decoder.Decode(&problem)
		return nil, &APIError{Code: problem.Error, Message: problem.Description, Status: response.StatusCode}
	}
	var token TokenResponse
	if err = decoder.Decode(&token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" || token.TokenType != "Bearer" || token.ExpiresIn <= 0 {
		return nil, errors.New("invalid token response")
	}
	token.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return &token, nil
}

// ClientCredentials obtains a service identity token for registered scopes.
func (c *Client) ClientCredentials(ctx context.Context, scopes []string) (*TokenResponse, error) {
	return c.tokenGrant(ctx, url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join(scopes, " ")}})
}

// ExchangeToken requires a previously authorized user delegation. The server
// checks the caller, source token, both account links, policy and user consent.
func (c *Client) ExchangeToken(ctx context.Context, subjectToken, resource string, scopes []string) (*TokenResponse, error) {
	if subjectToken == "" || resource == "" || len(scopes) == 0 {
		return nil, errors.New("subject token, resource and scopes required")
	}
	result, err := c.tokenGrant(ctx, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subjectToken}, "subject_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"}, "resource": {resource}, "audience": {resource}, "scope": {strings.Join(scopes, " ")}})
	if err != nil {
		return nil, err
	}
	if result.IssuedTokenType != "urn:ietf:params:oauth:token-type:access_token" {
		return nil, errors.New("wrong exchanged token type")
	}
	return result, nil
}
