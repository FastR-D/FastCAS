package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLinkReplayUsesTheTransactionConnection(t *testing.T) {
	s, user, session := fixture(t)
	ctx := context.Background()
	intent, err := s.CreateLinkIntent(ctx, "write", "replayed-local-account", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	authorize(t, s, session, intent.Nonce)
	config := s.DB.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	limited := &core.Store{DB: pool, Keys: s.Keys, Dev: s.Dev}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	prepared, err := limited.PrepareLink(ctx, "write", intent.ID, intent.Nonce, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := limited.PrepareLink(ctx, "write", intent.ID, intent.Nonce, user.ID)
	if err != nil {
		t.Fatalf("prepare replay cannot acquire its own transaction connection: %v", err)
	}
	if replayed.ID != prepared.ID || replayed.Version != prepared.Version {
		t.Fatal("prepare replay changed the relationship")
	}
	active, err := limited.ActivateLink(ctx, "write", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err = limited.ActivateLink(ctx, "write", prepared.ID)
	if err != nil {
		t.Fatalf("activation replay cannot acquire its own transaction connection: %v", err)
	}
	if replayed.ID != active.ID || replayed.Version != active.Version || replayed.State != "active" {
		t.Fatal("activation replay changed the relationship")
	}
	revoked, err := limited.RevokeLink(ctx, "write", active.ID, active.Version)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != "revoked" {
		t.Fatal("relationship was not revoked")
	}
	if _, err := limited.ActivateLink(ctx, "write", active.ID); err == nil {
		t.Fatal("revoked relationship was reactivated")
	}
}
