package httpapi

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// resumeAuthorization completes ordinary identity sign-in using the current
// browser session. The registered callback, PKCE, nonce and state are still
// validated and the provider still creates a one-time authorization code.
// It returns false only when the request needs a credential or consent UI.
func (s *Server) resumeAuthorization(w http.ResponseWriter, r *http.Request, id string, session *core.BrowserSession) bool {
	request, err := s.Store.AuthRequestByID(r.Context(), id)
	if err != nil {
		failure(w, err)
		return true
	}
	a := request.(*core.Authorization)
	silent := slices.Contains(a.Request.Prompt, oidc.PromptNone)
	if session == nil || !a.SessionFresh(session) || (a.RequestedSubject != "" && a.RequestedSubject != session.IdentityID) {
		if silent {
			op.AuthRequestError(w, r, a, oidc.ErrLoginRequired(), s.Provider)
			return true
		}
		return false
	}
	intent, err := s.Store.IntentForAuthorization(r.Context(), a)
	if err != nil {
		failure(w, err)
		return true
	}
	interactive := intent != nil || slices.Contains(a.Request.Prompt, oidc.PromptConsent) || slices.Contains(a.Request.Prompt, oidc.PromptSelectAccount)
	needsConsent, err := s.Store.AuthorizationConsentRequired(r.Context(), a, session.IdentityID)
	if err != nil {
		failure(w, err)
		return true
	}
	interactive = interactive || needsConsent
	if interactive {
		if silent {
			op.AuthRequestError(w, r, a, oidc.ErrInteractionRequired(), s.Provider)
			return true
		}
		return false
	}
	if err = s.Store.CompleteAuthorization(r.Context(), id, session); err != nil {
		failure(w, err)
		return true
	}
	http.Redirect(w, r, op.AuthCallbackURL(s.Provider)(r.Context(), id), http.StatusSeeOther)
	return true
}

// Native form redirects must permit the exact registered RP origin throughout
// password, GitHub and MFA navigation, even when no consent document is shown.
func (s *Server) authorizationFormPolicy(w http.ResponseWriter, a *core.Authorization) bool {
	redirect, err := url.Parse(a.GetRedirectURI())
	if err != nil || redirect.Scheme == "" || redirect.Host == "" || redirect.User != nil || strings.ContainsAny(redirect.Host, " \t\r\n;'\"\\") {
		failure(w, core.ErrForbidden)
		return false
	}
	origins := redirect.Scheme + "://" + redirect.Host
	if s.github != nil {
		origins += " https://github.com"
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; form-action 'self' "+origins+"; frame-ancestors 'none'; base-uri 'none'")
	return true
}
