package core_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func fixture(t *testing.T) (*core.Store, *core.Identity, *core.BrowserSession) {
	t.Helper()
	s := testutil.Store(t)
	ctx := context.Background()
	err := s.RegisterClient(ctx, core.Client{ID: "write", Name: "FastWrite", Development: true, Redirects: []string{"http://127.0.0.1:3003/callback"}, Scopes: []string{"openid", "profile", "email", "offline_access"}}, "test-client-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateIdentity(ctx, "alice@example.test", "Alice", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, u, session
}
func authorize(t *testing.T, s *core.Store, session *core.BrowserSession, nonce string) *core.Authorization {
	t.Helper()
	ctx := context.Background()
	challenge := sha256.Sum256([]byte(core.RandomToken()))
	request, err := s.CreateAuthRequest(ctx, &oidc.AuthRequest{ClientID: "write", RedirectURI: "http://127.0.0.1:3003/callback", ResponseType: oidc.ResponseTypeCode, Scopes: []string{"openid", "offline_access"}, State: core.RandomToken(), Nonce: nonce, CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: oidc.CodeChallengeMethodS256}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteAuthorization(ctx, request.GetID(), session); err != nil {
		t.Fatal(err)
	}
	request, err = s.AuthRequestByID(ctx, request.GetID())
	if err != nil {
		t.Fatal(err)
	}
	return request.(*core.Authorization)
}

func TestAuthorizationCodeAtomicConsumption(t *testing.T) {
	s, _, session := fixture(t)
	ctx := context.Background()
	a := authorize(t, s, session, core.RandomToken())
	if err := s.SaveAuthCode(ctx, a.ID, "one-time-code"); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := s.AuthRequestByCode(ctx, "one-time-code"); err == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("code consumed %d times", wins.Load())
	}
}
func TestRefreshReplayRevokesDescendants(t *testing.T) {
	s, _, session := fixture(t)
	ctx := context.Background()
	a := authorize(t, s, session, core.RandomToken())
	_, refresh, _, err := s.CreateAccessAndRefreshTokens(ctx, a, "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.TokenRequestByRefreshToken(ctx, refresh)
	if err != nil {
		t.Fatal(err)
	}
	access, next, _, err := s.CreateAccessAndRefreshTokens(ctx, req, refresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActiveToken(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TokenRequestByRefreshToken(ctx, refresh); err == nil {
		t.Fatal("replay accepted")
	}
	if _, err = s.TokenRequestByRefreshToken(ctx, next); err == nil {
		t.Fatal("descendant refresh survived replay")
	}
	if _, err = s.ActiveToken(ctx, access); err == nil {
		t.Fatal("descendant access survived replay")
	}
}
func TestLinkProofConflictRevocationAndLocalIdentitySurvival(t *testing.T) {
	s, u, session := fixture(t)
	ctx := context.Background()
	intent, err := s.CreateLinkIntent(ctx, "write", "local-user-1", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareLink(ctx, "write", intent.ID, intent.Nonce, u.ID); err == nil {
		t.Fatal("unconfirmed link prepared")
	}
	authorize(t, s, session, intent.Nonce)
	link, err := s.PrepareLink(ctx, "write", intent.ID, intent.Nonce, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveLink(ctx, "write", u.ID); err == nil {
		t.Fatal("prepared link used to log in")
	}
	link, err = s.ActivateLink(ctx, "write", link.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolveLink(ctx, "write", u.ID)
	if err != nil || resolved.LocalRef != "local-user-1" {
		t.Fatal("local identity mapping changed")
	}
	second, err := s.CreateLinkIntent(ctx, "write", "local-user-2", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	authorize(t, s, session, second.Nonce)
	if _, err = s.PrepareLink(ctx, "write", second.ID, second.Nonce, u.ID); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("duplicate subject not rejected: %v", err)
	}
	if _, err = s.RevokeLink(ctx, "write", link.ID, link.Version+1); !errors.Is(err, core.ErrConflict) {
		t.Fatal("stale version accepted")
	}
	revoked, err := s.RevokeLink(ctx, "write", link.ID, link.Version)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Version <= link.Version {
		t.Fatal("version did not advance")
	}
	if _, err = s.ActivateLink(ctx, "write", link.ID); err == nil {
		t.Fatal("revoked relationship resurrected")
	}
	if _, err = s.ResolveLink(ctx, "write", u.ID); err == nil {
		t.Fatal("revoked link resolves")
	}
	if _, err = s.Authenticate(ctx, u.Email, "correct horse battery staple", "local-test"); err != nil {
		t.Fatalf("unlink removed identity credentials: %v", err)
	}
	var events int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("expected one revoke event: %d %v", events, err)
	}
}
func TestLinkIdempotencyIsolationAndPasswords(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	key := core.RandomToken()
	one, err := s.CreateLinkIntent(ctx, "write", "local-1", key)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.CreateLinkIntent(ctx, "write", "local-1", key)
	if err != nil || one.ID != two.ID {
		t.Fatal("same transaction not idempotent")
	}
	if _, err = s.CreateLinkIntent(ctx, "write", "local-2", key); !errors.Is(err, core.ErrConflict) {
		t.Fatal("idempotency payload mismatch accepted")
	}
	if _, err = s.Link(ctx, "wrong-client", one.ID); err == nil {
		t.Fatal("cross-client read")
	}
	encoded, err := core.HashPassword("a long enough test password")
	if err != nil {
		t.Fatal(err)
	}
	if !core.PasswordMatches("a long enough test password", encoded) || core.PasswordMatches("wrong", encoded) {
		t.Fatal("password verification failed")
	}
	if core.PasswordMatches("password", "$argon2id$v=19$m=999999999,t=3,p=2$AA$AA") {
		t.Fatal("unbounded password parameters accepted")
	}
}

func TestExpiredPreparedReservationCanBeReplaced(t *testing.T) {
	s, u, session := fixture(t)
	ctx := context.Background()
	first, err := s.CreateLinkIntent(ctx, "write", "original-local-user", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	authorize(t, s, session, first.Nonce)
	if _, err = s.PrepareLink(ctx, "write", first.ID, first.Nonce, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE link_intents SET expires_at=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Maintenance(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateLinkIntent(ctx, "write", "original-local-user", core.RandomToken())
	if err != nil {
		t.Fatal(err)
	}
	authorize(t, s, session, second.Nonce)
	if _, err = s.PrepareLink(ctx, "write", second.ID, second.Nonce, u.ID); err != nil {
		t.Fatal(err)
	}
	active, err := s.ActivateLink(ctx, "write", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE link_intents SET expires_at=now()-interval '1 day' WHERE id=$1`, active.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Maintenance(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveLink(ctx, "write", u.ID); err != nil {
		t.Fatal("active link incorrectly expired")
	}
}
