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

func TestFastTaskAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and built FastTask")
	}
	store, provider := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{
		ID: "task", Name: "FastTask", Development: true,
		Redirects: []string{origin + "/api/v1/auth/fastcas/callback"},
		Scopes:    []string{"openid", "profile", "email"},
	}, clientSecret); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "FastTask", "web", "dist", "index.html")); err != nil {
		t.Fatal("build FastTask web before browser contract: npm run build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-race", "./internal/httpapi", "-run", "^TestFastCASBrowserAgainstProvider$", "-count=1", "-v")
	cmd.Dir = filepath.Join(root, "FastTask")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+provider.issuer, "FASTCAS_CONTRACT_TASK_ORIGIN="+origin, "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastTask browser contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
