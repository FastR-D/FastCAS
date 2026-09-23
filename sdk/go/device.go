package fastcas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type DeviceLoginResult struct {
	Identity    Identity
	AccessToken string
	IDToken     string
	ExpiresIn   int
}

func (c *Client) deviceForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Code        string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(data, &failure)
		if failure.Code == "" {
			failure.Code = "request_failed"
		}
		return &APIError{Code: failure.Code, Message: failure.Description, Status: resp.StatusCode}
	}
	return json.Unmarshal(data, out)
}

// DeviceAuthorize starts an RFC 8628 public-client flow. Display both URI and code.
func (c *Client) DeviceAuthorize(ctx context.Context, scopes []string) (*DeviceAuthorization, error) {
	if c.config.ClientSecret != "" {
		return nil, errors.New("device pairing requires a public client")
	}
	_, metadata, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	endpointURL, err := endpoint(metadata.DeviceAuthorizationEndpoint, c.config.AllowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	if endpointURL.Scheme+"://"+endpointURL.Host != c.config.Issuer {
		return nil, errors.New("device endpoint must belong to issuer")
	}
	form := url.Values{"client_id": {c.config.ClientID}, "scope": {strings.Join(scopes, " ")}}
	var start DeviceAuthorization
	if err = c.deviceForm(ctx, endpointURL.String(), form, &start); err != nil {
		return nil, err
	}
	verification, err := endpoint(start.VerificationURI, c.config.AllowLoopbackHTTP)
	if err != nil || verification.Scheme+"://"+verification.Host != c.config.Issuer || start.DeviceCode == "" || start.UserCode == "" || start.ExpiresIn <= 0 || start.Interval <= 0 {
		return nil, errors.New("invalid device authorization response")
	}
	return &start, nil
}

// PollDevice makes one token request. The caller respects Interval and slow_down.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (*DeviceLoginResult, error) {
	if c.config.ClientSecret != "" {
		return nil, errors.New("device pairing requires a public client")
	}
	p, metadata, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{"client_id": {c.config.ClientID}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode}}
	var token struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err = c.deviceForm(ctx, metadata.TokenEndpoint, form, &token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" || token.IDToken == "" || token.ExpiresIn <= 0 {
		return nil, errors.New("incomplete device token response")
	}
	id, err := p.Verifier(&oidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: []string{"RS256"}}).Verify(c.ctx(ctx), token.IDToken)
	if err != nil {
		return nil, err
	}
	access, err := c.VerifyAccessToken(ctx, token.AccessToken, c.config.ClientID, "openid")
	if err != nil {
		return nil, err
	}
	if id.Subject != access.Subject || access.Service {
		return nil, errors.New("device identity mismatch")
	}
	return &DeviceLoginResult{Identity: Identity{Issuer: c.config.Issuer, Subject: id.Subject}, AccessToken: token.AccessToken, IDToken: token.IDToken, ExpiresIn: token.ExpiresIn}, nil
}
