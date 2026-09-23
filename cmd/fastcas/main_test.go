package main

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestCheckOutboxCommandAlertsOnStalePendingAndDead(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var schema string
	if err := s.DB.QueryRow(ctx, `SHOW search_path`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(s.DB.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("options", "-csearch_path="+schema)
	dsn.RawQuery = query.Encode()
	t.Setenv("FASTCAS_DATABASE_URL", dsn.String())
	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })
	os.Args = []string{"fastcas", "check-outbox"}
	if err := run(); err != nil {
		t.Fatalf("empty queue alerted: %v", err)
	}
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,created_at) VALUES('stale','receiver','{}',now()-interval '45 seconds')`); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("stale pending event did not alert")
	}
	if _, err := s.DB.Exec(ctx, `UPDATE outbox SET dead_at=now() WHERE id='stale'`); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("dead letter did not alert")
	}
	os.Args = []string{"fastcas", "check-outbox", "-max-dead=1"}
	if err := run(); err != nil {
		t.Fatalf("tuned dead-letter threshold failed: %v", err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,created_at,delivered_at) VALUES('slow','receiver','{}',now()-interval '60 seconds',now()-interval '10 seconds')`); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("slow p95 delivery did not alert")
	}
	os.Args = []string{"fastcas", "check-outbox", "-max-dead=1", "-max-p95-delivery=1m"}
	if err := run(); err != nil {
		t.Fatalf("tuned delivery threshold failed: %v", err)
	}
}
