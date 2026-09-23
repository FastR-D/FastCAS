package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestOutboxTelemetryMeasuresPendingDeadAndRecentDelivery(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,created_at,available_at,delivered_at,dead_at) VALUES
 ('pending','receiver','{}',now()-interval '45 seconds',now()-interval '10 seconds',NULL,NULL),
 ('scheduled','receiver','{}',now()-interval '60 seconds',now()+interval '1 hour',NULL,NULL),
 ('dead','receiver','{}',now()-interval '1 hour',now()-interval '1 hour',NULL,now()),
 ('delivered','receiver','{}',now()-interval '40 seconds',now()-interval '40 seconds',now()-interval '10 seconds',NULL)`); err != nil {
		t.Fatal(err)
	}
	report, err := s.OutboxTelemetry(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pending != 2 || report.Due != 1 || report.Dead != 1 || report.RecentDelivered != 1 {
		t.Fatalf("wrong outbox counts: %+v", report)
	}
	if report.OldestPendingAgeSeconds < 59 || report.OldestDueAgeSeconds < 9 || report.P95DeliverySeconds < 29 || report.P95DeliverySeconds > 31 {
		t.Fatalf("wrong outbox latency: %+v", report)
	}
	short, err := s.OutboxTelemetry(ctx, time.Second)
	if err != nil || short.RecentDelivered != 0 || short.P95DeliverySeconds != 0 {
		t.Fatalf("old delivery counted in short window: %+v %v", short, err)
	}
}
