package fastcas

import (
	"context"
	"errors"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

type AccountLinkEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Link Link   `json:"link"`
}

type Notification struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Link    *Link  `json:"link,omitempty"`
	Subject string `json:"subject,omitempty"`
	Status  string `json:"status,omitempty"`
	Version int64  `json:"version,omitempty"`
}

// HandleEvent verifies a signed notification before invoking apply. The caller
// must atomically deduplicate event.ID, compare Link.Version, and update local
// FastCAS bindings/sessions. Already committed duplicates should return nil.
// Return 2xx to the webhook only after apply commits successfully.
func (c *Client) HandleEvent(ctx context.Context, raw string, apply func(context.Context, AccountLinkEvent) error) error {
	if apply == nil {
		return errors.New("invalid event handler")
	}
	return c.HandleNotification(ctx, raw, func(ctx context.Context, event Notification) error {
		if event.Type != "account_link.revoked" || event.Link == nil {
			return errors.New("unsupported account-link event")
		}
		return apply(ctx, AccountLinkEvent{ID: event.ID, Type: event.Type, Link: *event.Link})
	})
}

// HandleNotification verifies either a binding revocation or a versioned
// identity status change. Apply must commit deduplication and local updates
// atomically; status changes must never disable a local project credential.
func (c *Client) HandleNotification(ctx context.Context, raw string, apply func(context.Context, Notification) error) error {
	if apply == nil || len(raw) > 65536 {
		return errors.New("invalid event handler or payload")
	}
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return err
	}
	if len(signed.Signatures) != 1 || signed.Signatures[0].Protected.ExtraHeaders["typ"] != "fastcas-event+jwt" {
		return errors.New("invalid event token type")
	}
	p, _, err := c.discovery(ctx)
	if err != nil {
		return err
	}
	verified, err := p.Verifier(&oidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: []string{"RS256"}}).Verify(c.ctx(ctx), raw)
	if err != nil {
		return err
	}
	var envelope struct {
		ID    string       `json:"jti"`
		Event Notification `json:"event"`
	}
	if err = verified.Claims(&envelope); err != nil {
		return err
	}
	event := envelope.Event
	event.ID = envelope.ID
	age := time.Since(verified.IssuedAt)
	if event.ID == "" || verified.IssuedAt.IsZero() || age > 5*time.Minute || age < -time.Minute {
		return errors.New("invalid event claims")
	}
	switch event.Type {
	case "account_link.revoked":
		if event.Link == nil || event.Link.ID == "" || event.Link.ClientID != c.config.ClientID || event.Link.Subject == "" || event.Link.LocalRef == "" || event.Link.State != "revoked" || event.Link.Version < 1 {
			return errors.New("invalid account-link event")
		}
	case "identity.status_changed":
		if event.Subject == "" || (event.Status != "active" && event.Status != "disabled") || event.Version < 2 || event.Link != nil {
			return errors.New("invalid identity status event")
		}
	default:
		return errors.New("unsupported event type")
	}
	return apply(ctx, event)
}
