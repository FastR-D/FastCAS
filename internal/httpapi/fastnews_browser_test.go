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

func TestFastNewsAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and FastNews")
	}
	store, provider := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{
		ID: "news", Name: "FastNews", Development: true,
		Redirects: []string{origin + "/api/auth/fastcas/callback"},
		Scopes:    []string{"openid", "profile", "email"},
	}, clientSecret); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(root, "FastNews", ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal("install FastNews test dependencies in .venv")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tests/fastcas_browser_contract.py")
	cmd.Dir = filepath.Join(root, "FastNews")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+provider.issuer, "FASTCAS_CONTRACT_NEWS_ORIGIN="+origin,
		"PYTHONPATH="+filepath.Join(root, "FastCAS", "sdk", "python", "src")+string(os.PathListSeparator)+os.Getenv("PYTHONPATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastNews browser contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
