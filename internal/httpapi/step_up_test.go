package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/pquerna/otp/totp"
)

func TestCodeOnlyStepUpRequiresLiveSessionOriginCSRFAndOneTimeProof(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	u, err := store.CreateIdentity(ctx, "stepup@example.test", "Step up", "test password long enough", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, csrfToken, err := store.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := store.BeginMFA(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := store.ConfirmMFA(ctx, u.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(store, "http://127.0.0.1:8900", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.ConfigureGitHub("fixture-id", "fixture-secret", ""); err != nil {
		t.Fatal(err)
	}
	stale := func() {
		t.Helper()
		_, err := store.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '10 minutes',mfa_at=now()-interval '10 minutes' WHERE id=$1`, session.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, body, origin, csrf, sessionToken string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:8900"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		if sessionToken != "" {
			r.AddCookie(&http.Cookie{Name: "fastcas_session", Value: sessionToken})
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		result := map[string]any{}
		_ = json.NewDecoder(w.Result().Body).Decode(&result)
		return w.Code, result
	}
	verify := func(value, origin, csrf, sessionToken string) (int, map[string]any) {
		raw, _ := json.Marshal(map[string]string{"code": value})
		return request("POST", "/api/v1/me/mfa/verify", string(raw), origin, csrf, sessionToken)
	}
	stale()
	if status, result := request("GET", "/api/v1/admin/identities", "", "", "", token); status != 403 || result["code"] != "admin_mfa_required" {
		t.Fatalf("missing challenge: %d %v", status, result)
	}
	for _, item := range []struct{ origin, csrf, token string }{{"https://other.example", csrfToken, token}, {"http://127.0.0.1:8900", "", token}, {"http://127.0.0.1:8900", csrfToken, ""}} {
		status, _ := verify(codes[0], item.origin, item.csrf, item.token)
		if status == 204 {
			t.Fatal("step-up bypassed session/origin/CSRF")
		}
	}
	if status, result := verify("invalid-code", "http://127.0.0.1:8900", csrfToken, token); status != 401 || result["code"] != "invalid_mfa_code" {
		t.Fatalf("invalid proof: %d %v", status, result)
	}
	if store.RequireRecentAccountAuth(ctx, u.ID, session.ID) == nil {
		t.Fatal("invalid proof refreshed authentication")
	}
	if status, result := verify(codes[0], "http://127.0.0.1:8900", csrfToken, token); status != 204 {
		t.Fatalf("code-only proof: %d %v", status, result)
	}
	if err = store.RequireRecentAccountAuth(ctx, u.ID, session.ID); err != nil {
		t.Fatal("step-up did not refresh primary and secondary proof", err)
	}
	if status, _ := request("GET", "/api/v1/admin/identities", "", "", "", token); status != 200 {
		t.Fatal("verified administrator still denied", status)
	}
	if status, result := request("POST", "/api/v1/me/github", "{}", "http://127.0.0.1:8900", csrfToken, token); status != 200 || result["authorization_url"] == "" {
		t.Fatalf("GitHub binding did not accept step-up: %d %v", status, result)
	}
	stale()
	if status, _ := verify(codes[0], "http://127.0.0.1:8900", csrfToken, token); status != 401 {
		t.Fatal("replayed recovery code accepted", status)
	}
	if store.RequireRecentAccountAuth(ctx, u.ID, session.ID) == nil {
		t.Fatal("replay refreshed session")
	}
	// Audit failure must roll back refreshed timestamps, even after consuming proof.
	if _, err = store.DB.Exec(ctx, `CREATE FUNCTION reject_step_up_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='session.step_up' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_step_up_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_step_up_audit()`); err != nil {
		t.Fatal(err)
	}
	if status, _ := verify(codes[1], "http://127.0.0.1:8900", csrfToken, token); status != 500 {
		t.Fatal("audit failure accepted", status)
	}
	if store.RequireRecentAccountAuth(ctx, u.ID, session.ID) == nil {
		t.Fatal("audit failure committed refreshed proof")
	}
	if _, err = store.DB.Exec(ctx, `DROP TRIGGER reject_step_up_audit ON audit_events; DROP FUNCTION reject_step_up_audit(); UPDATE browser_sessions SET revoked_at=now()`); err != nil {
		t.Fatal(err)
	}
	if status, _ := verify(codes[2], "http://127.0.0.1:8900", csrfToken, token); status != 401 {
		t.Fatal("revoked session accepted", status)
	}
}
