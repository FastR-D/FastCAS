package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
)

func TestReusableInvitationSharesQuotaAcrossOperations(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	_, err := s.DB.Exec(ctx, `
 INSERT INTO identities(id,email,name,password_hash) VALUES('legacy','fr-legacy@fastresearch.invalid','旧账户','');
 INSERT INTO fastresearch_imports(local_account_id,identity_id,legacy_name) VALUES('local','legacy','旧账户');`)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := s.IssueClaim(ctx, "admin", "fr-legacy@fastresearch.invalid", "claimed@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	const password = "a long test password"
	if _, err := s.RedeemClaim(ctx, proof, "", "旧账户", password); !errors.Is(err, core.ErrExpired) {
		t.Fatalf("binding proof used without the reusable invitation: %v", err)
	}
	code, err := s.IssueReusableInvite(ctx, "admin", core.ReusableInviteOptions{
		Operations: []string{"register", "claim"}, MaxUses: 3, ExpiresInHours: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"first@example.test", "second@example.test"} {
		if _, err := s.RedeemReusableRegistration(ctx, code, email, "New account", password); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RedeemReusableRegistration(ctx, code, "first@example.test", "Duplicate", password); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("duplicate registration returned %v", err)
	}
	if _, err := s.RedeemClaim(ctx, code, proof, "错误姓名", password); !errors.Is(err, core.ErrExpired) {
		t.Fatalf("wrong account name returned %v", err)
	}
	user, err := s.RedeemClaim(ctx, code, proof, "旧账户", password)
	if err != nil || user.ID != "legacy" {
		t.Fatalf("binding did not preserve account identity: %v", err)
	}
	if _, err := s.RedeemReusableRegistration(ctx, code, "third@example.test", "Exhausted", password); !errors.Is(err, core.ErrExpired) {
		t.Fatalf("exhausted invitation was accepted: %v", err)
	}
}

func TestReusableInvitationScopesAndConcurrentRedemption(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	code, err := s.IssueReusableInvite(ctx, "admin", core.ReusableInviteOptions{
		Operations: []string{"claim"}, MaxUses: 1, ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const password = "a long test password"
	if _, err := s.RedeemReusableRegistration(ctx, code, "blocked@example.test", "Blocked", password); !errors.Is(err, core.ErrExpired) {
		t.Fatalf("binding-only invitation allowed registration: %v", err)
	}
	code, err = s.IssueReusableInvite(ctx, "admin", core.ReusableInviteOptions{
		Operations: []string{"register"}, MaxUses: 1, ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, email := range []string{"concurrent1@example.test", "concurrent2@example.test"} {
		go func() {
			_, err := s.RedeemReusableRegistration(ctx, code, email, "Concurrent", password)
			results <- err
		}()
	}
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, core.ErrExpired)) || (second == nil && errors.Is(first, core.ErrExpired))) {
		t.Fatalf("one last use must succeed and one must fail: %v, %v", first, second)
	}
}
