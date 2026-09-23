package core_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func linkedLogoutFixture(t *testing.T, s *core.Store, receiver string) (*core.Identity, string) {
	t.Helper()
	ctx := context.Background()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true,
		Redirects: []string{receiver + "/callback"}, Scopes: []string{"openid"}, BackchannelURL: receiver, EventsURL: receiver}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateIdentity(ctx, "linked@example.test", "Linked User", "correct horse battery staple", "member")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO link_intents(id,client_id,local_ref,idempotency_key,payload_hash,nonce,subject,state,expires_at)
	 VALUES('link','receiver','local-user','idempotency-long-enough','hash','nonce',$1,'active',now()+interval '1 hour')`, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO account_links(id,client_id,local_ref,subject,state) VALUES('link','receiver','local-user',$1,'active')`, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return user, token
}

func TestLogoutAllQueuesSignedBackchannelNotification(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var calls int
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("incorrect logout content type")
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		token, err := jwt.ParseSigned(form.Get("logout_token"), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		signed, err := jose.ParseSigned(form.Get("logout_token"), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil || signed.Signatures[0].Protected.ExtraHeaders["typ"] != "logout+jwt" {
			t.Error("incorrect logout token type")
		}
		var claims jwt.Claims
		var extra struct {
			Events map[string]map[string]any `json:"events"`
			Nonce  string                    `json:"nonce"`
		}
		if err = token.Claims(&s.Keys.Private.PublicKey, &claims, &extra); err != nil {
			t.Error(err)
		}
		if err = claims.Validate(jwt.Expected{Issuer: "https://cas.example", AnyAudience: jwt.Audience{"receiver"}, Time: time.Now()}); err != nil {
			t.Error(err)
		}
		if claims.Subject == "" || claims.ID == "" || extra.Nonce != "" || len(extra.Events) != 1 || extra.Events["http://schemas.openid.net/event/backchannel-logout"] == nil {
			t.Error("invalid standard logout claims")
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	user, token := linkedLogoutFixture(t, s, receiver.URL)
	if err := s.LogoutAll(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(ctx, token); err == nil {
		t.Fatal("browser session survived global logout")
	}
	var id, kind string
	if err := s.DB.QueryRow(ctx, `SELECT id,kind FROM outbox WHERE client_id='receiver'`).Scan(&id, &kind); err != nil || kind != "logout" {
		t.Fatalf("logout queue: %s %s %v", id, kind, err)
	}
	worked, err := s.DeliverEvent(ctx, "https://cas.example")
	if err != nil || !worked || calls != 1 {
		t.Fatalf("delivery: %v %v calls=%d", worked, err, calls)
	}
	var delivered bool
	if err = s.DB.QueryRow(ctx, `SELECT delivered_at IS NOT NULL FROM outbox WHERE id=$1`, id).Scan(&delivered); err != nil || !delivered {
		t.Fatalf("not delivered: %v", err)
	}
}

func TestIdentityDisableQueuesLogoutAtomically(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	user, token := linkedLogoutFixture(t, s, "http://127.0.0.1:19191")
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION fail_logout_audit() RETURNS trigger AS $$ BEGIN IF NEW.action='identity.disabled' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql;
	 CREATE TRIGGER fail_logout_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_logout_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("expected atomic audit failure, got %v", err)
	}
	if _, err := s.Session(ctx, token); err != nil {
		t.Fatalf("disable rollback revoked session: %v", err)
	}
	var queued int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind='logout'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatal("failed disable left logout notice")
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind='event'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatal("failed disable left identity event")
	}
	if _, err := s.DB.Exec(ctx, `DROP TRIGGER fail_logout_audit ON audit_events; DROP FUNCTION fail_logout_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(ctx, token); err == nil {
		t.Fatal("disabled identity session active")
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind='logout'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("missing logout notice: %d %v", queued, err)
	}
	var payload []byte
	if err := s.DB.QueryRow(ctx, `SELECT payload FROM outbox WHERE kind='event' AND client_id='receiver'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type, Subject, Status string
		Version               int64
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "identity.status_changed" || event.Subject != user.ID || event.Status != "disabled" || event.Version != 2 {
		t.Fatalf("invalid identity event: %+v", event)
	}
	if err := s.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind='event'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("duplicate status event: %d %v", queued, err)
	}
	if err := s.SetIdentityStatus(ctx, "admin", user.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT payload FROM outbox WHERE kind='event' AND payload->>'status'='active'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Status != "active" || event.Version != 3 {
		t.Fatalf("invalid reactivation event: %+v", event)
	}
}

func TestIdentityStatusDeliversSignedVersionedNotification(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var received int
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/jwt" {
			w.WriteHeader(415)
			return
		}
		body, _ := io.ReadAll(r.Body)
		signed, err := jose.ParseSigned(string(body), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil || len(signed.Signatures) != 1 || signed.Signatures[0].Protected.ExtraHeaders["typ"] != "fastcas-event+jwt" {
			w.WriteHeader(400)
			return
		}
		var claims jwt.Claims
		var extra struct {
			Event struct {
				Type, Subject, Status string
				Version               int64
			} `json:"event"`
		}
		parsed, err := jwt.ParseSigned(string(body), []jose.SignatureAlgorithm{jose.RS256})
		if err == nil {
			err = parsed.Claims(&s.Keys.Private.PublicKey, &claims)
		}
		if err == nil {
			decoded, verifyErr := signed.Verify(&s.Keys.Private.PublicKey)
			if verifyErr != nil {
				err = verifyErr
			} else {
				err = json.Unmarshal(decoded, &extra)
			}
		}
		if err != nil ||
			claims.Issuer != "https://cas.example" || !claims.Audience.Contains("receiver") || claims.ID == "" ||
			extra.Event.Type != "identity.status_changed" || extra.Event.Status != "disabled" || extra.Event.Version != 2 || extra.Event.Subject == "" {
			w.WriteHeader(400)
			return
		}
		received++
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	user, _ := linkedLogoutFixture(t, s, receiver.URL)
	if err := s.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		worked, err := s.DeliverEvent(ctx, "https://cas.example")
		if err != nil || !worked {
			t.Fatalf("delivery failed: %v %v", worked, err)
		}
	}
	if received != 1 {
		t.Fatalf("signed identity event delivered %d times", received)
	}
}

func TestLogoutSessionQueuesSIDAndPreservesOtherSessions(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	user, first := linkedLogoutFixture(t, s, "http://127.0.0.1:19191")
	second, _, err := s.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.Session(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LogoutSession(ctx, user.ID, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, first); err == nil {
		t.Fatal("target session survived")
	}
	if _, err = s.Session(ctx, second); err != nil {
		t.Fatal("other session revoked", err)
	}
	var sid, subject string
	if err = s.DB.QueryRow(ctx, `SELECT payload->>'sid',payload->>'subject' FROM outbox WHERE kind='logout'`).Scan(&sid, &subject); err != nil || sid != active.ID || subject != user.ID {
		t.Fatalf("scoped notice %q %q %v", sid, subject, err)
	}
	if err = s.LogoutSession(ctx, user.ID, active.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind='logout'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate logout queued: %d %v", count, err)
	}
}
