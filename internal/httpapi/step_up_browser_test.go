package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/pquerna/otp/totp"
)

func TestStepUpAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1")
	}
	ctx := context.Background()
	store := testutil.Store(t)
	user, err := store.CreateIdentity(ctx, "stepup-admin@example.test", "Step-up admin", "fixture password not entered twice", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, csrfToken, err := store.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := store.BeginMFA(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := store.ConfirmMFA(ctx, user.ID, session.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	expire := func() error {
		_, e := store.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '10 minutes',mfa_at=now()-interval '10 minutes' WHERE id=$1`, session.ID)
		return e
	}
	if err = expire(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	issuer := "http://" + server.Listener.Addr().String()
	api, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	api.ConsoleEnabled = true
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	console, err := httpapi.Console(api, filepath.Join(root, "web/dist"))
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clock control belongs only to this isolated test server, never application routes.
		if r.Method == "POST" && r.URL.Path == "/__test/expire-proof" {
			cookie, e := r.Cookie("fastcas_session")
			if e != nil || cookie.Value != token {
				w.WriteHeader(403)
				return
			}
			if expire() != nil {
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(204)
			return
		}
		console.ServeHTTP(w, r)
	})
	server.Start()
	defer server.Close()
	rawCodes, _ := json.Marshal(codes)
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "node", "tests/step_up_contract.mjs")
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ORIGIN="+issuer, "FASTCAS_STEP_UP_SESSION="+token, "FASTCAS_STEP_UP_CSRF="+csrfToken, "FASTCAS_STEP_UP_CODES="+string(rawCodes))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("step-up browser contract: %v\n%s", err, output)
	}
	t.Log(string(output))
	var count int
	if err = store.DB.QueryRow(ctx, `SELECT count(*) FROM one_time_credentials WHERE email='stepup-invite@example.test' AND kind='invite'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cancel/retry created duplicate credentials: %d %v", count, err)
	}
}
