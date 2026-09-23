package fastcas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Introspection struct {
	Active   bool     `json:"active"`
	Subject  string   `json:"sub,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	Audience []string `json:"aud,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

// IntrospectToken checks current central revocation state. A resource server
// must still verify the JWT issuer, audience, token type and scopes itself.
func (c *Client) IntrospectToken(ctx context.Context, token string) (*Introspection, error) {
	if c.config.ClientSecret == "" || token == "" {
		return nil, errors.New("confidential client credential and token required")
	}
	_, metadata, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	endpointURL, err := endpoint(metadata.IntrospectionEndpoint, c.config.AllowLoopbackHTTP)
	if err != nil || endpointURL.Scheme+"://"+endpointURL.Host != c.config.Issuer {
		return nil, errors.New("introspection endpoint must belong to issuer")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL.String(), strings.NewReader(url.Values{"token": {token}}.Encode()))
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
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code        string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&problem)
		if problem.Code == "" {
			problem.Code = "introspection_failed"
		}
		return nil, &APIError{Code: problem.Code, Message: problem.Description, Status: response.StatusCode}
	}
	var result struct {
		Active *bool `json:"active"`
		Introspection
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || result.Active == nil {
		return nil, errors.New("invalid introspection response")
	}
	result.Introspection.Active = *result.Active
	return &result.Introspection, nil
}
