package fastcas

import (
	"context"
	"errors"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

type LogoutNotice struct {
	ID        string
	Subject   string
	SessionID string
	ExpiresAt time.Time
}

// VerifyLogout verifies a standard OIDC back-channel logout token. The caller
// atomically deduplicates ID and revokes only matching FastCAS-source sessions.
func (c *Client) VerifyLogout(ctx context.Context, raw string) (*LogoutNotice, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, errors.New("invalid logout token length")
	}
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, err
	}
	if len(signed.Signatures) != 1 || signed.Signatures[0].Protected.ExtraHeaders["typ"] != "logout+jwt" {
		return nil, errors.New("invalid logout token type")
	}
	p, _, err := c.discovery(ctx)
	if err != nil {
		return nil, err
	}
	verified, err := p.Verifier(&oidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: []string{"RS256"}}).Verify(c.ctx(ctx), raw)
	if err != nil {
		return nil, err
	}
	var claims struct {
		ID      string                    `json:"jti"`
		Subject string                    `json:"sub"`
		SID     string                    `json:"sid"`
		Nonce   *string                   `json:"nonce"`
		Events  map[string]map[string]any `json:"events"`
	}
	if err = verified.Claims(&claims); err != nil {
		return nil, err
	}
	age := time.Since(verified.IssuedAt)
	if claims.ID == "" || claims.Subject == "" || claims.Nonce != nil || verified.IssuedAt.IsZero() || age > 5*time.Minute || age < -time.Minute ||
		len(claims.Events) != 1 || claims.Events["http://schemas.openid.net/event/backchannel-logout"] == nil {
		return nil, errors.New("invalid logout claims")
	}
	return &LogoutNotice{ID: claims.ID, Subject: claims.Subject, SessionID: claims.SID, ExpiresAt: verified.Expiry}, nil
}
