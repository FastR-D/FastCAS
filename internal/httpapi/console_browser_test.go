package httpapi_test

import (
	"context"
	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestConsoleAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and built React console")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	dist := filepath.Join(root, "web", "dist")
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("build web console before browser contract: npm ci && npm run build")
	}
	store := testutil.Store(t)
	server := httptest.NewUnstartedServer(nil)
	issuer := "http://" + server.Listener.Addr().String()
	api, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	api.ConsoleEnabled = true
	handler, err := httpapi.Console(api, dist)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	defer server.Close()
	ctx := context.Background()
	if _, err = store.CreateIdentity(ctx, "alice@example.test", "Alice", "correct horse battery staple", "member"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateIdentity(ctx, "admin@example.test", "Administrator", "administrator long password", "admin"); err != nil {
		t.Fatal(err)
	}
	if err = store.RegisterClient(ctx, core.Client{ID: "browser-contract-receiver", Name: "Receiver", Development: true, Redirects: []string{"http://127.0.0.1/callback"}, Scopes: []string{"openid"}}, "long-enough-test-client-secret-32-characters"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateServiceAccountAs(ctx, "test-admin", "seed-service", "Seed service", []string{"insight:publish"}, []string{"research-api"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO identities(id,email,name,password_hash,role,status)
 SELECT 'zz-user-'||lpad(n::text,3,'0'),'zz-user-'||lpad(n::text,3,'0')||'@example.test','Paged user',i.password_hash,'member','active'
 FROM identities i CROSS JOIN generate_series(1,105) AS n WHERE i.email='alice@example.test';
 INSERT INTO applications(id,name,secret_hash,config)
 SELECT 'zz-app-'||lpad(n::text,3,'0'),'Paged app',a.secret_hash,
 a.config || jsonb_build_object('id','zz-app-'||lpad(n::text,3,'0'),'name','Paged app')
 FROM applications a CROSS JOIN generate_series(1,105) AS n WHERE a.id='browser-contract-receiver';
 INSERT INTO applications(id,name,secret_hash,config)
 SELECT 'zz-service-'||lpad(n::text,3,'0'),'Paged service',a.secret_hash,
 a.config || jsonb_build_object('id','zz-service-'||lpad(n::text,3,'0'),'name','Paged service')
 FROM applications a CROSS JOIN generate_series(1,105) AS n WHERE a.id='seed-service'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload,attempts,dead_at,last_status) VALUES('browser-contract-dead','browser-contract-receiver','{"private":"never-return-this"}',12,now(),503); INSERT INTO outbox_attempts(event_id,attempt,outcome,http_status,duration_ms) VALUES('browser-contract-dead',12,'http_error',503,42)`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO audit_events(actor,action,target)
 SELECT 'browser-contract','audit.page','audit-page-'||lpad(n::text,3,'0') FROM generate_series(1,105) AS n`); err != nil {
		t.Fatal(err)
	}
	invite, err := store.IssueCredential(ctx, "test-admin", "invite", "invited@example.test")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := store.IssueCredential(ctx, "test-admin", "recovery", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "node", "tests/console_contract.mjs")
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ORIGIN="+issuer, "FASTCAS_CONTRACT_INVITE="+invite, "FASTCAS_CONTRACT_RECOVERY="+recovery)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser contract failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
