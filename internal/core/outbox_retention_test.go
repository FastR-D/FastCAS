package core_test

import (
	"context"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestMaintenanceRetainsOpenOutboxAndPrunesOldDeliveredHistory(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,created_at,delivered_at,dead_at) VALUES
 ('old-delivered','receiver','{}',now()-interval '91 days',now()-interval '91 days',NULL),
 ('recent-delivered','receiver','{}',now()-interval '1 day',now()-interval '1 day',NULL),
 ('old-dead','receiver','{}',now()-interval '91 days',NULL,now()-interval '91 days'),
 ('old-pending','receiver','{}',now()-interval '91 days',NULL,NULL);
 INSERT INTO outbox_attempts(event_id,attempt,outcome,duration_ms) VALUES('old-delivered',1,'delivered',10),('old-dead',1,'http_error',10)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Maintenance(ctx); err != nil {
		t.Fatal(err)
	}
	var events, attempts int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if events != 3 || attempts != 1 {
		t.Fatalf("retention deleted pending/dead or kept old successful history: events=%d attempts=%d", events, attempts)
	}
}
