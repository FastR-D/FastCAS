// Package ginadapter contains opt-in Gin handlers for signed FastCAS callbacks
// and resource tokens. It never installs a global login gate or maps CAS
// subjects to application users; applications retain local account policy.
package ginadapter

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	fastcas "github.com/FastR-D/FastCAS/sdk/go"
	"github.com/gin-gonic/gin"
)

const MaxNotificationBytes = 65536
const ClaimsKey = "fastcas.access_claims"

type ClientProvider func() (*fastcas.Client, error)
type ApplyEvent func(context.Context, fastcas.Notification) error
type ApplyLogout func(context.Context, fastcas.LogoutNotice) error

func mediaType(c *gin.Context, expected string) bool {
	if strings.ToLower(strings.TrimSpace(strings.SplitN(c.GetHeader("Content-Type"), ";", 2)[0])) != expected {
		c.AbortWithStatus(http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

func boundedBody(c *gin.Context) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxNotificationBytes+1))
	if err != nil || len(raw) > MaxNotificationBytes {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return raw, true
}

// Events verifies the signature before invoking apply. The application must
// atomically deduplicate event IDs and change only FastCAS-source sessions.
func Events(provider ClientProvider, apply ApplyEvent) gin.HandlerFunc {
	return func(c *gin.Context) {
		if provider == nil || apply == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if !mediaType(c, "application/jwt") {
			return
		}
		raw, ok := boundedBody(c)
		if !ok {
			return
		}
		client, err := provider()
		if err != nil || client == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		started := false
		err = client.HandleNotification(c.Request.Context(), string(raw), func(ctx context.Context, event fastcas.Notification) error {
			started = true
			return apply(ctx, event)
		})
		if err != nil {
			if started {
				c.AbortWithStatus(http.StatusServiceUnavailable)
			} else {
				c.AbortWithStatus(http.StatusUnauthorized)
			}
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// Logout verifies an OIDC back-channel token before apply. apply commits
// notice.ID deduplication and CAS-source session revocation together.
func Logout(provider ClientProvider, apply ApplyLogout) gin.HandlerFunc {
	return func(c *gin.Context) {
		if provider == nil || apply == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if !mediaType(c, "application/x-www-form-urlencoded") {
			return
		}
		raw, ok := boundedBody(c)
		if !ok {
			return
		}
		values, err := url.ParseQuery(string(raw))
		if err != nil || len(values) != 1 || len(values["logout_token"]) != 1 || values.Get("logout_token") == "" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		client, err := provider()
		if err != nil || client == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		notice, err := client.VerifyLogout(c.Request.Context(), values.Get("logout_token"))
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if err = apply(c.Request.Context(), *notice); err != nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// RequireAccessToken is mounted only on resource routes selected by the app.
// Local user status, link mapping, ACLs and service-recipient policy remain the
// application's responsibility. Introspection observes immediate revocation.
func RequireAccessToken(provider ClientProvider, audience string, scopes []string, introspect bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if provider == nil || audience == "" {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" || strings.Contains(parts[1], ",") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		client, err := provider()
		if err != nil || client == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		claims, err := client.VerifyAccessToken(c.Request.Context(), parts[1], audience, scopes...)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if introspect {
			state, err := client.IntrospectToken(c.Request.Context(), parts[1])
			if err != nil {
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return
			}
			if !state.Active || state.Subject != claims.Subject || state.ClientID != claims.ClientID {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		c.Set(ClaimsKey, claims)
		c.Next()
	}
}

// Claims returns only a previously verified access-token principal.
func Claims(c *gin.Context) (*fastcas.AccessClaims, bool) {
	value, exists := c.Get(ClaimsKey)
	claims, ok := value.(*fastcas.AccessClaims)
	return claims, exists && ok
}
