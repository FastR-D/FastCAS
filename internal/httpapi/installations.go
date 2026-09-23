package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/go-chi/chi/v5"
)

func bearer(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return ""
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return ""
	}
	return token
}

func (s *Server) registerDeviceInstallation(w http.ResponseWriter, r *http.Request) {
	token, err := s.Store.DeviceAccessToken(r.Context(), bearer(r), s.Issuer)
	if err != nil {
		failure(w, core.ErrUnauthorized)
		return
	}
	var input struct {
		InstallationID string `json:"installation_id"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	item, secret, err := s.Store.RegisterDeviceInstallation(r.Context(), token, input.InstallationID)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 201, map[string]any{"installation": item, "management_secret": secret})
}

func (s *Server) myDeviceInstallations(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	items, next, err := s.Store.DeviceInstallations(r.Context(), session.IdentityID, r.URL.Query().Get("before"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"installations": items, "next_cursor": next})
}

func (s *Server) revokeMyDeviceInstallation(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	if time.Since(session.AuthenticatedAt) > 5*time.Minute {
		failure(w, core.ErrUnauthorized)
		return
	}
	if err := s.Store.RevokeDeviceInstallation(r.Context(), chi.URLParam(r, "id"), session.IdentityID); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) deviceInstallationStatus(w http.ResponseWriter, r *http.Request) {
	item, err := s.Store.DeviceInstallationBySecret(r.Context(), chi.URLParam(r, "id"), bearer(r))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, item)
}

func (s *Server) revokeDeviceInstallation(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeDeviceInstallationWithSecret(r.Context(), chi.URLParam(r, "id"), bearer(r)); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
