package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
)

type statusContract struct {
	clientID, name, callback, eventsPath string
	failFirstDelivery                    bool
	command                              func(root, issuer, origin, signal string) *exec.Cmd
}

func runIdentityStatusContract(t *testing.T, spec statusContract) {
	t.Helper()
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires project dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	callback := spec.callback
	if !strings.HasPrefix(callback, "http") {
		callback = origin + callback
	}
	eventsURL := origin + spec.eventsPath
	var deliveries atomic.Int32
	if spec.failFirstDelivery {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if deliveries.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if err != nil {
				w.WriteHeader(502)
				return
			}
			forward, err := http.NewRequestWithContext(r.Context(), http.MethodPost, origin+spec.eventsPath, bytes.NewReader(body))
			if err != nil {
				w.WriteHeader(502)
				return
			}
			forward.Header.Set("Content-Type", r.Header.Get("Content-Type"))
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := client.Do(forward)
			if err != nil {
				w.WriteHeader(502)
				return
			}
			defer response.Body.Close()
			w.WriteHeader(response.StatusCode)
		}))
		defer proxy.Close()
		eventsURL = proxy.URL
	}
	if spec.clientID == "write" {
		if _, err = store.DB.Exec(context.Background(), `UPDATE applications SET config=jsonb_set(config,'{redirect_uris}',to_jsonb(ARRAY[$1::text])) || jsonb_build_object('events_uri',$2::text) WHERE id='write'`, callback, eventsURL); err != nil {
			t.Fatal(err)
		}
	} else {
		if err = store.RegisterClient(context.Background(), core.Client{ID: spec.clientID, Name: spec.name, Development: true, Redirects: []string{callback}, EventsURL: eventsURL, Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				_, _ = store.DeliverEvent(ctx, b.issuer)
			}
		}
	}()
	signal := filepath.Join(t.TempDir(), "cas-session-ready")
	ackSignal := signal + "-delivery-ack"
	configured := spec.command(root, b.issuer, origin, signal)
	cmd := exec.CommandContext(ctx, configured.Path, configured.Args[1:]...)
	// command's directory and environment are supplied by the project adapter.
	cmd.Dir = configured.Dir
	cmd.Env = append(configured.Env, "FASTCAS_CONTRACT_STATUS_ACK_SIGNAL="+ackSignal)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		if _, err = os.Stat(signal); err == nil {
			break
		}
		select {
		case err = <-done:
			t.Fatalf("%s ended before CAS session: %v\n%s", spec.name, err, output.String())
		case <-ctx.Done():
			t.Fatalf("%s readiness timeout", spec.name)
		case <-time.After(50 * time.Millisecond):
		}
	}
	var subject string
	if err = store.DB.QueryRow(ctx, `SELECT id FROM identities WHERE email='alice@example.test'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if err = store.SetIdentityStatus(ctx, "test-admin", subject, "disabled"); err != nil {
		t.Fatal(err)
	}
	eventID, elapsed := identityStatusDeliveryLatency(t, store, ctx, spec.clientID)
	if elapsed > 30 {
		t.Fatalf("%s status event reached project after %.2fs", spec.name, elapsed)
	}
	if err = os.WriteFile(ackSignal, []byte("delivered"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("%s status contract: %v\n%s", spec.name, err, output.String())
		}
	case <-ctx.Done():
		t.Fatalf("%s status contract timeout", spec.name)
	}
	if !strings.Contains(output.String(), "identity status contract passed") {
		t.Fatalf("%s did not confirm signed status event:\n%s", spec.name, output.String())
	}
	if spec.failFirstDelivery {
		attempts, err := store.OutboxAttempts(ctx, eventID)
		if err != nil {
			t.Fatal(err)
		}
		if deliveries.Load() != 2 || len(attempts) != 2 || attempts[0].Outcome != "delivered" || attempts[1].Outcome != "http_error" || attempts[1].HTTPStatus == nil || *attempts[1].HTTPStatus != 503 || elapsed < 2 || elapsed > 30 {
			t.Fatalf("%s retry did not revoke within 30s: deliveries=%d attempts=%+v elapsed=%.2fs", spec.name, deliveries.Load(), attempts, elapsed)
		}
		t.Logf("%s first 503, signed retry reached project and revoked CAS session in %.2fs", spec.name, elapsed)
	} else {
		t.Logf("%s signed status event reached project in %.2fs", spec.name, elapsed)
	}
}

func identityStatusDeliveryLatency(t *testing.T, store *core.Store, ctx context.Context, clientID string) (string, float64) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var eventID string
		var elapsed float64
		err := store.DB.QueryRow(ctx, `SELECT id,EXTRACT(EPOCH FROM delivered_at-created_at)::double precision FROM outbox WHERE client_id=$1 AND kind='event' AND payload->>'type'='identity.status_changed' AND delivered_at IS NOT NULL`, clientID).Scan(&eventID, &elapsed)
		if err == nil {
			return eventID, elapsed
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s status delivery was not acknowledged: %v", clientID, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
