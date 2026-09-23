package core_test

import (
	"context"
	"errors"
	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeadEventRetryIsAtomicAndFenced(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,attempts,dead_at) VALUES('dead','receiver','{"type":"account_link.revoked"}',12,now()),('leased','receiver','{}',12,now()),('delivered','receiver','{}',12,now()); UPDATE outbox SET lease_until=now()+interval '1 hour',lease_token='active-worker' WHERE id='leased'; UPDATE outbox SET delivered_at=now() WHERE id='delivered'`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"leased", "delivered", "missing"} {
		if err := s.RetryDeadEvent(ctx, "admin", id); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("retry %s: %v", id, err)
		}
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.RetryDeadEvent(ctx, "admin", "dead")
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, core.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("multiple retries committed")
	}
	var audits, attempts int
	var payload string
	if err := s.DB.QueryRow(ctx, `SELECT attempts,payload::text FROM outbox WHERE id='dead'`).Scan(&attempts, &payload); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || payload != `{"type": "account_link.revoked"}` {
		t.Fatal("retry changed payload or failed reset")
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='outbox.retry' AND target='dead'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("retry missing unique audit", err)
	}
	rows, err := s.OutboxStatuses(ctx, "", false)
	if err != nil || len(rows) != 3 {
		t.Fatal("list", err)
	}
	// A failed audit must roll back requeueing as well.
	if _, err = s.DB.Exec(ctx, `UPDATE outbox SET dead_at=now(),attempts=12 WHERE id='dead'; CREATE FUNCTION reject_retry_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER fail_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_retry_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = s.RetryDeadEvent(ctx, "admin", "dead"); err == nil {
		t.Fatal("audit failure accepted")
	}
	if err = s.DB.QueryRow(ctx, `SELECT attempts FROM outbox WHERE id='dead' AND dead_at IS NOT NULL`).Scan(&attempts); err != nil || attempts != 12 {
		t.Fatal("audit failure lost dead letter", err)
	}
}
