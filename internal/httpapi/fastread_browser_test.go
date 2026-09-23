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

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestFastReadAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and built FastRead")
	}
	store, provider := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{
		ID: "read", Name: "FastRead", Development: true,
		Redirects: []string{origin + "/api/auth/fastcas/callback"},
		Scopes:    []string{"openid", "profile", "email"},
	}, clientSecret); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "FastRead", "fastread-frontend", "dist", "index.html")); err != nil {
		t.Fatal("build FastRead frontend before browser contract")
	}
	python := filepath.Join(root, "FastRead", ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal("install FastRead browser test dependencies in .venv")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "scripts/fastcas_browser_contract.py")
	cmd.Dir = filepath.Join(root, "FastRead")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+provider.issuer, "FASTCAS_CONTRACT_READ_ORIGIN="+origin,
		"PYTHONPATH="+filepath.Join(root, "FastRead", "backend")+string(os.PathListSeparator)+filepath.Join(root, "FastCAS", "sdk", "python", "src")+string(os.PathListSeparator)+os.Getenv("PYTHONPATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastRead browser contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
