package core_test

import (
	"context"
	"github.com/FastR-D/FastCAS/internal/core"
	"testing"
)

func TestCommittedAuditCarriesRequestContext(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := core.WithAuditRequestID(context.Background(), "server-generated-request")
	if err := s.RegisterClient(ctx, core.Client{ID: "audit-context", Name: "Audit context", Development: true, Redirects: []string{"http://127.0.0.1:3003/callback"}, Scopes: []string{"openid"}}, "isolated-credential-at-least-32-characters"); err != nil {
		t.Fatal(err)
	}
	var requestID, result string
	if err := s.DB.QueryRow(ctx, `SELECT request_id,result FROM audit_events WHERE action='application.create' AND target='audit-context'`).Scan(&requestID, &result); err != nil {
		t.Fatal(err)
	}
	if requestID != "server-generated-request" || result != "success" {
		t.Fatalf("request=%q result=%q", requestID, result)
	}
}
