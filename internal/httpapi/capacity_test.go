package httpapi_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// This opt-in smoke test exercises distinct browser sessions against the real
// provider and PostgreSQL. It reports measurements, not a production SLO.
func TestCapacityLoginAndRefreshBurst(t *testing.T) {
	if os.Getenv("FASTCAS_LOAD_TEST") != "1" {
		t.Skip("set FASTCAS_LOAD_TEST=1 with an isolated PostgreSQL test database")
	}
	store, provider := setup(t)
	// Each account and local source address is distinct so the intentional
	// per-account and per-address password rate limits remain in force.
	const sessions = 64
	const password = "capacity smoke account password"
	for i := range sessions {
		email := fmt.Sprintf("capacity-%02d@example.test", i)
		if _, err := store.CreateIdentity(context.Background(), email, "Capacity user", password, "member"); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var loginTimes, refreshTimes []time.Duration
	wallStart := time.Now()
	t.Run("independent_browser_sessions", func(t *testing.T) {
		for i := range sessions {
			t.Run(fmt.Sprintf("session_%02d", i), func(t *testing.T) {
				t.Parallel()
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(i+2))}}
				transport := &http.Transport{DialContext: dialer.DialContext}
				t.Cleanup(transport.CloseIdleConnections)
				client := &http.Client{Jar: jar, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				b := browser{t: t, http: client, issuer: provider.issuer}
				loginStart := time.Now()
				code, verifier, _ := authorizeCodeFor(t, b, "openid profile email offline_access", fmt.Sprintf("capacity-%02d@example.test", i), password)
				status, tokens := exchange(t, b, url.Values{
					"grant_type": {"authorization_code"}, "code": {code},
					"redirect_uri": {"http://127.0.0.1:3003/callback"}, "code_verifier": {verifier},
				})
				if status != http.StatusOK {
					t.Fatalf("code exchange status %d: %v", status, tokens)
				}
				loginElapsed := time.Since(loginStart)
				refreshToken, ok := tokens["refresh_token"].(string)
				if !ok || refreshToken == "" {
					t.Fatal("code exchange omitted refresh token")
				}
				refreshStart := time.Now()
				status, rotated := exchange(t, b, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
				if status != http.StatusOK || rotated["refresh_token"] == "" {
					t.Fatalf("refresh status %d", status)
				}
				mu.Lock()
				loginTimes = append(loginTimes, loginElapsed)
				refreshTimes = append(refreshTimes, time.Since(refreshStart))
				mu.Unlock()
			})
		}
	})
	if len(loginTimes) != sessions || len(refreshTimes) != sessions {
		t.Fatalf("completed %d/%d login and %d/%d refresh sessions", len(loginTimes), sessions, len(refreshTimes), sessions)
	}
	sort.Slice(loginTimes, func(i, j int) bool { return loginTimes[i] < loginTimes[j] })
	sort.Slice(refreshTimes, func(i, j int) bool { return refreshTimes[i] < refreshTimes[j] })
	p95 := func(values []time.Duration) time.Duration { return values[(len(values)*95+99)/100-1] }
	t.Logf("sessions=%d wall=%s login_p50=%s login_p95=%s refresh_p50=%s refresh_p95=%s",
		sessions, time.Since(wallStart), loginTimes[sessions/2], p95(loginTimes), refreshTimes[sessions/2], p95(refreshTimes))
}
