package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestDurableEventDelivery(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		token, err := jwt.ParseSigned(string(raw), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var claims jwt.Claims
		var event struct {
			Event struct {
				Type string `json:"type"`
			} `json:"event"`
		}
		if err = token.Claims(&s.Keys.Private.PublicKey, &claims, &event); err != nil {
			t.Error(err)
		}
		if err = claims.Validate(jwt.Expected{Issuer: "https://cas.example", AnyAudience: jwt.Audience{"receiver"}, Time: time.Now()}); err != nil {
			t.Error(err)
		}
		if claims.ID != "test-event" || event.Event.Type != "account_link.revoked" {
			t.Error("incorrect event envelope")
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{receiver.URL + "/callback"}, Scopes: []string{"openid"}, EventsURL: receiver.URL}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload) VALUES('test-event','receiver','{"type":"account_link.revoked"}')`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.DeliverEvent(ctx, "https://cas.example"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("deliveries = %d", calls.Load())
	}
	var delivered bool
	if err := s.DB.QueryRow(ctx, `SELECT delivered_at IS NOT NULL FROM outbox WHERE id='test-event'`).Scan(&delivered); err != nil || !delivered {
		t.Fatalf("not acknowledged: %v", err)
	}
	history, err := s.OutboxAttempts(ctx, "test-event")
	if err != nil || len(history) != 1 || history[0].Attempt != 1 || history[0].Outcome != "delivered" || history[0].HTTPStatus == nil || *history[0].HTTPStatus != 204 {
		t.Fatalf("missing successful delivery history: %+v %v", history, err)
	}
}

func TestEventRedirectIsRetriedWithoutFollowing(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer receiver.Close()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{receiver.URL + "/callback"}, Scopes: []string{"openid"}, EventsURL: receiver.URL}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,attempts) VALUES('retry','receiver','{}',11)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverEvent(ctx, "https://cas.example"); err != nil {
		t.Fatal(err)
	}
	var dead bool
	var status int
	if err := s.DB.QueryRow(ctx, `SELECT dead_at IS NOT NULL,last_status FROM outbox WHERE id='retry'`).Scan(&dead, &status); err != nil {
		t.Fatal(err)
	}
	if leaked.Load() != 0 || !dead || status != 307 {
		t.Fatalf("unsafe redirect or missing dead letter: %d %v %d", leaked.Load(), dead, status)
	}
	history, err := s.OutboxAttempts(ctx, "retry")
	if err != nil || len(history) != 1 || history[0].Outcome != "http_error" || history[0].HTTPStatus == nil || *history[0].HTTPStatus != 307 {
		t.Fatalf("redirect delivery history: %+v %v", history, err)
	}
	if worked, err := s.DeliverEvent(ctx, "https://cas.example"); err != nil || worked {
		t.Fatalf("dead event retried: %v %v", worked, err)
	}
}

func TestDeliveryAttemptHistorySurvivesDeadLetterRetry(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Receiver", Development: true, Redirects: []string{receiver.URL + "/callback"}, Scopes: []string{"openid"}, EventsURL: receiver.URL}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,attempts) VALUES('recover','receiver','{"secret":"never-return-this"}',11)`); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.DeliverEvent(ctx, "https://cas.example"); err != nil || !worked {
		t.Fatalf("first delivery: %v %v", worked, err)
	}
	if err := s.RetryDeadEvent(ctx, "admin", "recover"); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.DeliverEvent(ctx, "https://cas.example"); err != nil || !worked {
		t.Fatalf("second delivery: %v %v", worked, err)
	}
	history, err := s.OutboxAttempts(ctx, "recover")
	if err != nil || len(history) != 2 {
		t.Fatalf("attempt history: %+v %v", history, err)
	}
	if history[0].Outcome != "delivered" || history[0].Attempt != 1 || history[1].Outcome != "http_error" || history[1].Attempt != 12 {
		t.Fatalf("attempt order or retry sequence: %+v", history)
	}
	encoded, err := json.Marshal(history)
	if err != nil || bytes.Contains(encoded, []byte("never-return-this")) {
		t.Fatal("attempt history leaked payload", err)
	}
}
