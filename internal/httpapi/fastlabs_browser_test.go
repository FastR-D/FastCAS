package httpapi_test

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestFastLabsAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and FastLabs")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "FastCAS", "web", "dist")
	store := testutil.Store(t)
	providerServer := httptest.NewUnstartedServer(nil)
	issuer := "http://" + providerServer.Listener.Addr().String()
	api, err := httpapi.New(store, issuer, true)
	if err != nil {
		t.Fatal(err)
	}
	api.ConsoleEnabled = true
	handler, err := httpapi.Console(api, dist)
	if err != nil {
		t.Fatal(err)
	}
	providerServer.Config.Handler = handler
	providerServer.Start()
	defer providerServer.Close()
	if _, err := store.CreateIdentity(context.Background(), "alice@example.test", "Alice", "correct horse battery staple", "member"); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterClient(context.Background(), core.Client{ID: "fastlab-device", Name: "FastLab desktop", Public: true,
		Scopes: []string{"openid", "profile"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
		t.Fatal(err)
	}
	user, err := store.Authenticate(context.Background(), "alice@example.test", "correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 55; index++ {
		id := fmt.Sprintf("browser-seed-%03d", index)
		ref := fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", index+1)
		if _, err := store.DB.Exec(context.Background(), `INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash,created_at)
		 VALUES($1,'fastlab-device',$2,$3::uuid,$4,$5,$6)`, id, user.ID, ref, "token-"+id, core.Hash("secret-"+id),
			time.Now().UTC().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	python := filepath.Join(root, "FastCAS", ".venv", "bin", "python")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tests/fastcas_browser_contract.py")
	cmd.Dir = filepath.Join(root, "FastLabs")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+issuer, "FASTCAS_CONTRACT_LABS_ORIGIN="+origin,
		"FASTCAS_CONTRACT_SUBJECT="+user.ID, "PYTHONPATH="+filepath.Join(root, "FastLabs")+string(os.PathListSeparator)+filepath.Join(root, "FastLabs", "tests")+string(os.PathListSeparator)+filepath.Join(root, "FastCAS", "sdk", "python", "src"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastLabs browser contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
