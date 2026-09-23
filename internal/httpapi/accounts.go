package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/go-chi/chi/v5"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func (s *Server) accountRoutes(r chi.Router) {
	r.Post("/api/v1/register", s.redeemInvite)
	r.Post("/api/v1/recover", s.redeemRecovery)
	r.Post("/api/v1/recover-mfa", s.redeemMFAReset)
	r.Post("/api/v1/me/mfa/enroll", s.enrollMFA)
	r.Post("/api/v1/me/mfa/confirm", s.confirmMFA)
	r.Post("/api/v1/me/mfa/recovery-codes", s.rotateMFARecoveryCodes)
	r.Post("/api/v1/me/reauthenticate", s.reauthenticate)
	r.Post("/api/v1/me/password", s.changePassword)
	r.Get("/api/v1/me/links", s.myLinks)
	r.Post("/api/v1/me/links/{id}/revoke", s.myRevoke)
	r.Get("/api/v1/me/device-installations", s.myDeviceInstallations)
	r.Post("/api/v1/me/device-installations/{id}/revoke", s.revokeMyDeviceInstallation)
	r.Post("/api/v1/device-installations", s.registerDeviceInstallation)
	r.Get("/api/v1/device-installations/{id}", s.deviceInstallationStatus)
	r.Post("/api/v1/device-installations/{id}/revoke", s.revokeDeviceInstallation)
	r.Get("/api/v1/me/sessions", s.mySessions)
	r.Post("/api/v1/me/sessions/{id}/revoke", s.revokeSession)
	r.Post("/api/v1/me/logout-all", s.logoutAll)
	r.Get("/api/v1/me/delegations", s.delegations)
	r.Post("/api/v1/me/delegations", s.setDelegation)
	r.Get("/api/v1/admin/identities", s.identities)
	r.Post("/api/v1/admin/identities/{id}/status", s.identityStatus)
	r.Post("/api/v1/admin/credentials", s.issueCredential)
	r.Get("/api/v1/admin/applications", s.applications)
	r.Post("/api/v1/admin/applications", s.createApplication)
	r.Post("/api/v1/admin/applications/{id}/callbacks", s.updateApplicationCallbacks)
	r.Post("/api/v1/admin/applications/{id}/capabilities", s.updateApplicationCapabilities)
	r.Get("/api/v1/admin/exchange-policies", s.exchangePolicies)
	r.Post("/api/v1/admin/exchange-policies", s.setExchangePolicy)
	r.Post("/api/v1/admin/applications/{id}/rotate-secret", s.rotateApplicationSecret)
	r.Get("/api/v1/admin/service-accounts", s.serviceAccounts)
	r.Post("/api/v1/admin/service-accounts", s.createServiceAccount)
	r.Post("/api/v1/admin/service-accounts/{id}/status", s.serviceAccountStatus)
	r.Post("/api/v1/admin/service-accounts/{id}/rotate-secret", s.rotateServiceAccountSecret)
	r.Get("/api/v1/admin/audit-events", s.audits)
	r.Get("/api/v1/admin/outbox", s.outboxStatus)
	r.Get("/api/v1/admin/outbox/{id}/attempts", s.outboxAttempts)
	r.Post("/api/v1/admin/outbox/{id}/retry", s.retryOutbox)
}
func (s *Server) authenticated(w http.ResponseWriter, r *http.Request, admin bool) (*core.BrowserSession, bool) {
	token := cookie(r, "fastcas_session")
	session, err := s.Store.Session(r.Context(), token)
	if err != nil {
		failure(w, err)
		return nil, false
	}
	if r.Method != "GET" && (!s.originOK(r) || s.Store.CheckCSRF(r.Context(), token, r.Header.Get("X-CSRF-Token")) != nil) {
		failure(w, core.ErrForbidden)
		return nil, false
	}
	if admin && s.Store.RequireAdmin(r.Context(), session) != nil {
		send(w, 403, map[string]string{"code": "admin_mfa_required", "message": "Administrator access requires recent multi-factor authentication"})
		return nil, false
	}
	return session, true
}
func (s *Server) enrollMFA(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	if time.Since(session.AuthenticatedAt) > 5*time.Minute {
		failure(w, core.ErrUnauthorized)
		return
	}
	uri, err := s.Store.BeginMFA(r.Context(), session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]string{"otpauth_uri": uri})
}
func (s *Server) confirmMFA(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if time.Since(session.AuthenticatedAt) > 5*time.Minute {
		failure(w, core.ErrUnauthorized)
		return
	}
	codes, err := s.Store.ConfirmMFA(r.Context(), session.IdentityID, session.ID, input.Code)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"recovery_codes": codes})
}
func (s *Server) rotateMFARecoveryCodes(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	codes, err := s.Store.RotateMFARecoveryCodes(r.Context(), session.IdentityID, session.ID)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"recovery_codes": codes})
}
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	u, err := s.Store.Identity(r.Context(), session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	if _, err = s.Store.Authenticate(r.Context(), u.Email, input.Password, s.peer(r)); err != nil {
		failure(w, err)
		return
	}
	enabled, err := s.Store.MFAEnabled(r.Context(), u.ID)
	if err != nil {
		failure(w, err)
		return
	}
	if enabled {
		if err = s.Store.VerifyMFA(r.Context(), u.ID, input.Code); err != nil {
			failure(w, err)
			return
		}
		if err = s.Store.MarkMFA(r.Context(), u.ID, session.ID); err != nil {
			failure(w, err)
			return
		}
	}
	_, err = s.Store.DB.Exec(r.Context(), `UPDATE browser_sessions SET authenticated_at=now() WHERE id=$1`, session.ID)
	if err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		Code            string `json:"code"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if err := s.Store.ChangePassword(r.Context(), session.IdentityID, session.ID, input.CurrentPassword, input.NewPassword, input.Code, s.peer(r)); err != nil {
		failure(w, err)
		return
	}
	s.setCookie(w, "fastcas_session", "", -1)
	s.setCookie(w, "fastcas_csrf", "", -1)
	w.WriteHeader(204)
}
func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request)   { s.redeem(w, r, "invite") }
func (s *Server) redeemRecovery(w http.ResponseWriter, r *http.Request) { s.redeem(w, r, "recovery") }
func (s *Server) redeemMFAReset(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) {
		failure(w, core.ErrForbidden)
		return
	}
	allowed, err := s.Store.Allow(r.Context(), "redeem:"+s.peer(r), 10, 5*time.Minute)
	if err != nil {
		failure(w, err)
		return
	}
	if !allowed {
		send(w, 429, map[string]string{"code": "rate_limited"})
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if err = s.Store.RedeemMFAReset(r.Context(), input.Token, input.Password, s.peer(r)); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]bool{"login_required": true})
}
func (s *Server) redeem(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.originOK(r) {
		failure(w, core.ErrForbidden)
		return
	}
	allowed, err := s.Store.Allow(r.Context(), "redeem:"+s.peer(r), 10, 5*time.Minute)
	if err != nil {
		failure(w, err)
		return
	}
	if !allowed {
		send(w, 429, map[string]string{"code": "rate_limited"})
		return
	}
	var input struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	user, err := s.Store.RedeemCredential(r.Context(), kind, input.Token, input.Name, input.Password)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"user": user, "login_required": true})
}
func (s *Server) myLinks(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,client_id,local_ref,subject,state,version,verified_at FROM account_links WHERE subject=$1 ORDER BY verified_at DESC LIMIT 200`, session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	items := []core.AccountLink{}
	for rows.Next() {
		var l core.AccountLink
		if err = rows.Scan(&l.ID, &l.ClientID, &l.LocalRef, &l.Subject, &l.State, &l.Version, &l.VerifiedAt); err != nil {
			failure(w, err)
			return
		}
		items = append(items, l)
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, items)
}
func (s *Server) myRevoke(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	if time.Since(session.AuthenticatedAt) > 5*time.Minute {
		failure(w, core.ErrUnauthorized)
		return
	}
	var client string
	err := s.Store.DB.QueryRow(r.Context(), `SELECT client_id FROM account_links WHERE id=$1 AND subject=$2`, chi.URLParam(r, "id"), session.IdentityID).Scan(&client)
	if err != nil {
		failure(w, core.ErrForbidden)
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	link, err := s.Store.RevokeLink(r.Context(), client, chi.URLParam(r, "id"), input.Version)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, link)
}
func (s *Server) mySessions(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,identity_id,authenticated_at,expires_at FROM browser_sessions WHERE identity_id=$1 AND revoked_at IS NULL AND expires_at>now() ORDER BY authenticated_at DESC LIMIT 100`, session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	list := []core.BrowserSession{}
	for rows.Next() {
		var item core.BrowserSession
		if err = rows.Scan(&item.ID, &item.IdentityID, &item.AuthenticatedAt, &item.ExpiresAt); err != nil {
			failure(w, err)
			return
		}
		list = append(list, item)
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, list)
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	target := chi.URLParam(r, "id")
	err := s.Store.LogoutSession(r.Context(), session.IdentityID, target)
	if err != nil {
		failure(w, err)
		return
	}
	if target == session.ID {
		for _, name := range []string{"fastcas_session", "fastcas_csrf", "fastcas_form", "fastcas_device_form"} {
			s.setCookie(w, name, "", -1)
		}
	}
	w.WriteHeader(204)
}
func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	if err := s.Store.LogoutAll(r.Context(), session.IdentityID); err != nil {
		failure(w, err)
		return
	}
	for _, name := range []string{"fastcas_session", "fastcas_csrf", "fastcas_form", "fastcas_device_form"} {
		s.setCookie(w, name, "", -1)
	}
	w.WriteHeader(204)
}
func (s *Server) identities(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,email,name,role,status,email_verified FROM identities WHERE id>$1 ORDER BY id LIMIT 100`, r.URL.Query().Get("after"))
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	list := []core.Identity{}
	for rows.Next() {
		var i core.Identity
		if err = rows.Scan(&i.ID, &i.Email, &i.Name, &i.Role, &i.Status, &i.EmailVerified); err != nil {
			failure(w, err)
			return
		}
		list = append(list, i)
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, list)
}
func (s *Server) identityStatus(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if err := s.Store.SetIdentityStatus(r.Context(), session.IdentityID, chi.URLParam(r, "id"), input.Status); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) issueCredential(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Kind  string `json:"kind"`
		Email string `json:"email"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	raw, err := s.Store.IssueCredential(r.Context(), session.IdentityID, input.Kind, input.Email)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 201, map[string]string{"token": raw, "delivery": "Deliver privately to the intended account owner. This token is shown only once."})
}
func (s *Server) applications(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT config FROM applications WHERE id>$1 ORDER BY id LIMIT 100`, r.URL.Query().Get("after"))
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	list := []json.RawMessage{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			failure(w, err)
			return
		}
		list = append(list, json.RawMessage(data))
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, list)
}
func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var c core.Client
	if decode(r, &c) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	secret := ""
	if !c.Public {
		secret = core.RandomToken()
	}
	if err := s.Store.RegisterClientAs(r.Context(), session.IdentityID, c, secret); err != nil {
		failure(w, err)
		return
	}
	send(w, 201, map[string]any{"client": c, "client_secret": secret})
}
func (s *Server) serviceAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	items, err := s.Store.ServiceAccounts(r.Context(), r.URL.Query().Get("after"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, items)
}
func (s *Server) createServiceAccount(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Scopes      []string `json:"scopes"`
		Resources   []string `json:"resources"`
		Development bool     `json:"development"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	secret, err := s.Store.CreateServiceAccountAs(r.Context(), session.IdentityID, input.ID, input.Name, input.Scopes, input.Resources, input.Development)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 201, map[string]any{"id": input.ID, "client_secret": secret})
}
func (s *Server) serviceAccountStatus(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Active *bool `json:"active"`
	}
	if decode(r, &input) != nil || input.Active == nil {
		failure(w, core.ErrForbidden)
		return
	}
	secret, err := s.Store.SetServiceAccountActiveAs(r.Context(), session.IdentityID, chi.URLParam(r, "id"), *input.Active)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"active": *input.Active, "client_secret": secret})
}
func (s *Server) rotateServiceAccountSecret(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		GraceSeconds *int `json:"grace_seconds"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	seconds := 600
	if input.GraceSeconds != nil {
		seconds = *input.GraceSeconds
	}
	if seconds < 0 || seconds > 3600 {
		failure(w, core.ErrForbidden)
		return
	}
	secret, err := s.Store.RotateServiceAccountSecretAs(r.Context(), session.IdentityID, chi.URLParam(r, "id"), time.Duration(seconds)*time.Second)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"client_secret": secret, "previous_valid_until_seconds": seconds})
}
func (s *Server) updateApplicationCallbacks(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		EventsURL      string `json:"events_uri"`
		BackchannelURL string `json:"backchannel_logout_uri"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	client, err := s.Store.UpdateClientCallbacksAs(r.Context(), session.IdentityID, chi.URLParam(r, "id"), input.EventsURL, input.BackchannelURL)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, client)
}
func (s *Server) updateApplicationCapabilities(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Grants    []oidc.GrantType `json:"grant_types"`
		Scopes    []string         `json:"scopes"`
		Resources []string         `json:"resources"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	client, err := s.Store.UpdateClientCapabilitiesAs(r.Context(), session.IdentityID, chi.URLParam(r, "id"), input.Grants, input.Scopes, input.Resources)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, client)
}

type exchangeChange struct {
	Caller   string `json:"caller_client"`
	Target   string `json:"target_client"`
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
	Active   bool   `json:"active"`
}

func (s *Server) exchangePolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	items, err := s.Store.ExchangePolicies(r.Context())
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, items)
}
func (s *Server) setExchangePolicy(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input exchangeChange
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if err := s.Store.SetExchangePolicy(r.Context(), session.IdentityID, input.Caller, input.Target, input.Resource, input.Scope, input.Active); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) delegations(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	items, err := s.Store.ExchangeOptionsFor(r.Context(), session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, items)
}
func (s *Server) setDelegation(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	var input exchangeChange
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if err := s.Store.SetExchangeConsent(r.Context(), session.IdentityID, input.Caller, input.Target, input.Resource, input.Scope, input.Active); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) audits(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			failure(w, core.ErrForbidden)
			return
		}
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,actor,action,target,created_at FROM audit_events WHERE ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT 100`, before)
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	type Event struct {
		ID        int64     `json:"id"`
		Actor     string    `json:"actor"`
		Action    string    `json:"action"`
		Target    string    `json:"target"`
		CreatedAt time.Time `json:"created_at"`
	}
	list := []Event{}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.CreatedAt); err != nil {
			failure(w, err)
			return
		}
		list = append(list, e)
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, list)
}

func (s *Server) rotateApplicationSecret(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	var input struct {
		GraceSeconds *int `json:"grace_seconds"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	seconds := 600
	if input.GraceSeconds != nil {
		seconds = *input.GraceSeconds
	}
	if seconds < 0 || seconds > 3600 {
		failure(w, core.ErrForbidden)
		return
	}
	secret, err := s.Store.RotateClientSecretAs(r.Context(), session.IdentityID, chi.URLParam(r, "id"), time.Duration(seconds)*time.Second)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"client_secret": secret, "previous_valid_until_seconds": seconds})
}
