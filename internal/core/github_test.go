package core_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestGitHubConcurrentRegistrationAndFlowConsumption(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	state, browser, err := s.NewGitHubFlow(ctx, core.GitHubFlow{Purpose: "login", Verifier: "proof"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var registered, consumed atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.RegisterGitHub(ctx, 123, "octocat", "octocat@example.test", "Octocat"); err == nil {
				registered.Add(1)
			} else if !errors.Is(err, core.ErrConflict) {
				t.Error(err)
			}
			if f, err := s.TakeGitHubFlow(ctx, state, browser, false); err == nil {
				consumed.Add(1)
				if f.Verifier != "proof" {
					t.Error("lost PKCE verifier")
				}
			} else if !errors.Is(err, core.ErrExpired) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if registered.Load() != 1 || consumed.Load() != 1 {
		t.Fatalf("registration winners=%d callback winners=%d", registered.Load(), consumed.Load())
	}
	var count int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM identities`).Scan(&count); err != nil || count != 1 {
		t.Fatal("partial registration left orphan identities", err)
	}
}
func TestGitHubInitialPasswordAndAuthenticationFencing(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.RegisterGitHub(ctx, 123, "octocat", "octocat@example.test", "Octocat")
	if err != nil {
		t.Fatal(err)
	}
	enabled, version, err := s.GitHubLoginState(ctx, u.ID)
	if err != nil || enabled {
		t.Fatal(err)
	}
	token, _, err := s.NewGitHubSession(ctx, u.ID, version, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '6 minutes' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, u.ID, session.ID, "", "first-local-password-123", "", "test"); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("stale session set initial password: %v", err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now() WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, u.ID, session.ID, "", "first-local-password-123", "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("initial password did not revoke session")
	}
	if _, err = s.Authenticate(ctx, u.Email, "first-local-password-123", "test"); err != nil {
		t.Fatal("new local password cannot log in", err)
	}
	if _, _, err = s.NewGitHubSession(ctx, u.ID, version, false); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old GitHub proof survived password change")
	}
	_, version, err = s.GitHubLoginState(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetIdentityStatus(ctx, "admin", u.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetIdentityStatus(ctx, "admin", u.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.NewGitHubSession(ctx, u.ID, version, false); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("old GitHub proof survived disable/re-enable")
	}
}
func TestGitHubDeletionAndRestoreFence(t *testing.T) {
	for _, scenario := range []string{"delete", "restore"} {
		t.Run(scenario, func(t *testing.T) {
			s := testutil.Store(t)
			ctx := context.Background()
			u, err := s.RegisterGitHub(ctx, 123, "octocat", "octocat@example.test", "Octocat")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.NewGitHubFlow(ctx, core.GitHubFlow{Purpose: "mfa", Subject: u.ID}); err != nil {
				t.Fatal(err)
			}
			if scenario == "delete" {
				err = s.DeleteIdentity(ctx, "admin", u.ID)
			} else {
				err = s.FenceRestoredState(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err = s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM github_accounts)+(SELECT count(*) FROM github_flows)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("GitHub credentials survived deletion/fence", err)
			}
		})
	}
}

func TestGitHubLogoutAllFencesPendingAuthentication(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	u, err := s.RegisterGitHub(ctx, 123, "octocat", "octocat@example.test", "Octocat")
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := s.GitHubLoginState(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LogoutAll(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.NewGitHubSession(ctx, u.ID, version, false); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatal("pending proof survived logout-all")
	}
}
