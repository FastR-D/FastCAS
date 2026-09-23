package core_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Opt-in backlog smoke test for the actual signed HTTP delivery worker.
func TestCapacityOutboxBacklog(t *testing.T) {
	if os.Getenv("FASTCAS_LOAD_TEST") != "1" {
		t.Skip("set FASTCAS_LOAD_TEST=1 with an isolated PostgreSQL test database")
	}
	const events, workers = 256, 8
	const issuer = "https://cas.example"
	s := testutil.Store(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var invalid, duplicate atomic.Int64
	var received sync.Map
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/jwt" {
			invalid.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			invalid.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		token, err := jwt.ParseSigned(string(raw), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			invalid.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var claims jwt.Claims
		var envelope struct {
			Event struct {
				Type string `json:"type"`
			} `json:"event"`
		}
		if err = token.Claims(&s.Keys.Private.PublicKey, &claims, &envelope); err != nil ||
			claims.Validate(jwt.Expected{Issuer: issuer, AnyAudience: jwt.Audience{"receiver"}, Time: time.Now()}) != nil ||
			envelope.Event.Type != "identity.status_changed" || claims.ID == "" {
			invalid.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, loaded := received.LoadOrStore(claims.ID, true); loaded {
			duplicate.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Capacity receiver", Development: true,
		Redirects: []string{receiver.URL + "/callback"}, Scopes: []string{"openid"}, EventsURL: receiver.URL},
		"capacity-receiver-secret-32-characters-long"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload)
 SELECT 'capacity-'||lpad(n::text,4,'0'),'receiver',jsonb_build_object('type','identity.status_changed','version',n)
 FROM generate_series(1,$1::integer) AS n`, events); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var wg sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			for {
				worked, err := s.DeliverEvent(ctx, issuer)
				if err != nil {
					errors <- err
					return
				}
				if !worked {
					return
				}
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var delivered, dead, attempts int
	var p95Seconds float64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE delivered_at IS NOT NULL),
 count(*) FILTER (WHERE dead_at IS NOT NULL),coalesce(sum(attempts),0),
 coalesce(percentile_disc(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM delivered_at-created_at)),0)
 FROM outbox`).Scan(&delivered, &dead, &attempts, &p95Seconds); err != nil {
		t.Fatal(err)
	}
	if delivered != events || dead != 0 || attempts != events || invalid.Load() != 0 || duplicate.Load() != 0 {
		t.Fatalf("delivered=%d/%d dead=%d attempts=%d invalid=%d duplicate=%d", delivered, events, dead, attempts, invalid.Load(), duplicate.Load())
	}
	t.Logf("events=%d workers=%d wall=%s delivery_p95=%s", events, workers, time.Since(start), time.Duration(p95Seconds*float64(time.Second)))
}

func TestCapacityOutboxRecovery(t *testing.T) {
	if os.Getenv("FASTCAS_LOAD_TEST") != "1" {
		t.Skip("set FASTCAS_LOAD_TEST=1 with an isolated PostgreSQL test database")
	}
	const events, workers = 128, 8
	s := testutil.Store(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var healthy atomic.Bool
	var failures, successes atomic.Int64
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			failures.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		successes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	if err := s.RegisterClient(ctx, core.Client{ID: "receiver", Name: "Recovering receiver", Development: true,
		Redirects: []string{receiver.URL + "/callback"}, Scopes: []string{"openid"}, EventsURL: receiver.URL},
		"capacity-receiver-secret-32-characters-long"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload)
 SELECT 'recovery-'||lpad(n::text,4,'0'),'receiver',jsonb_build_object('type','identity.status_changed','version',n)
 FROM generate_series(1,$1::integer) AS n`, events); err != nil {
		t.Fatal(err)
	}
	drainDue := func() {
		t.Helper()
		var wg sync.WaitGroup
		errors := make(chan error, workers)
		for range workers {
			wg.Go(func() {
				for {
					worked, err := s.DeliverEvent(ctx, "https://cas.example")
					if err != nil {
						errors <- err
						return
					}
					if !worked {
						return
					}
				}
			})
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			t.Fatal(err)
		}
	}
	start := time.Now()
	drainDue()
	var pending, attempts int
	var retryAt time.Time
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE delivered_at IS NULL AND dead_at IS NULL),
 coalesce(sum(attempts),0),max(available_at) FROM outbox`).Scan(&pending, &attempts, &retryAt); err != nil {
		t.Fatal(err)
	}
	if pending != events || attempts != events || failures.Load() != events {
		t.Fatalf("503 phase pending=%d attempts=%d failures=%d", pending, attempts, failures.Load())
	}
	healthy.Store(true)
	if wait := time.Until(retryAt); wait > 0 {
		select {
		case <-time.After(wait + 50*time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	drainDue()
	var delivered, dead, history int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE delivered_at IS NOT NULL),
 count(*) FILTER (WHERE dead_at IS NOT NULL),coalesce(sum(attempts),0) FROM outbox`).Scan(&delivered, &dead, &attempts); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM outbox_attempts`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if delivered != events || dead != 0 || attempts != events*2 || history != events*2 || successes.Load() != events {
		t.Fatalf("recovery delivered=%d/%d dead=%d attempts=%d history=%d successes=%d", delivered, events, dead, attempts, history, successes.Load())
	}
	t.Logf("events=%d workers=%d failure_status=503 retry_wall=%s", events, workers, time.Since(start))
}
