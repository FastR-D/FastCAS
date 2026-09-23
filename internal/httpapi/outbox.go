package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (s *Server) outboxStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	events, err := s.Store.OutboxStatuses(r.Context(), r.URL.Query().Get("before"), r.URL.Query().Get("state") != "all")
	if err != nil {
		failure(w, err)
		return
	}
	next := ""
	if len(events) == 100 {
		next = events[len(events)-1].ID
	}
	send(w, 200, map[string]any{"events": events, "next_cursor": next})
}
func (s *Server) outboxAttempts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticated(w, r, true); !ok {
		return
	}
	attempts, err := s.Store.OutboxAttempts(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"attempts": attempts})
}
func (s *Server) retryOutbox(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticated(w, r, true)
	if !ok {
		return
	}
	if err := s.Store.RetryDeadEvent(r.Context(), session.IdentityID, chi.URLParam(r, "id")); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]string{"status": "queued"})
}
