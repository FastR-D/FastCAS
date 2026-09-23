package httpapi_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFastWriteAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and built FastWrite")
	}
	store, provider := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if _, err := store.DB.Exec(context.Background(), `UPDATE applications SET config=jsonb_set(config,'{redirect_uris}',jsonb_build_array($1::text)) WHERE id='write'`, origin+"/api/auth/fastcas/callback"); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "FastWrite", "apps", "web", "dist", "index.html")); err != nil {
		t.Fatal("build FastWrite web before browser contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "scripts/fastcas-browser-contract.ts")
	cmd.Dir = filepath.Join(root, "FastWrite")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+provider.issuer, "FASTCAS_CONTRACT_WRITE_ORIGIN="+origin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastWrite browser contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
