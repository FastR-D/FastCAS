package httpapi_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestFastResearchAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome and built FastResearch")
	}
	store, provider := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{ID: "research", Name: "FastResearch", Development: true, Redirects: []string{origin + "/api/auth/fastcas/callback"}, Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	research := filepath.Join(root, "FastResearch")
	if _, err := os.Stat(filepath.Join(research, "dist", "index.html")); err != nil {
		t.Fatal("build FastResearch before browser contract: npm run build")
	}
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	server := exec.CommandContext(ctx, "node", filepath.Join(research, "server", "index.mjs"))
	var serverLog bytes.Buffer
	server.Stdout = &serverLog
	server.Stderr = &serverLog
	server.Dir = directory
	server.Env = append(os.Environ(),
		"HOST=127.0.0.1", "PORT="+strings.TrimPrefix(origin, "http://127.0.0.1:"),
		"FASTRESEARCH_STATIC_DIR="+filepath.Join(research, "dist"), "FASTRESEARCH_DATA_DIR="+directory,
		"FASTRESEARCH_ACCOUNT_DATABASE="+filepath.Join(directory, "accounts.sqlite"),
		"ADMIN_USERNAME=admin", "ADMIN_PASSWORD=local-admin-password", "FASTRESEARCH_PUBLIC_URL="+origin,
		"FASTRESEARCH_COOKIE_DOMAIN=", "FASTRESEARCH_COOKIE_NAME=fr_session",
		"FASTRESEARCH_FASTCAS_ISSUER="+provider.issuer, "FASTRESEARCH_FASTCAS_CLIENT_ID=research",
		"FASTRESEARCH_FASTCAS_CLIENT_SECRET="+clientSecret,
		"FASTRESEARCH_FASTCAS_REDIRECT_URI="+origin+"/api/auth/fastcas/callback",
		"FASTRESEARCH_FASTCAS_ALLOW_LOOPBACK_HTTP=true")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if server.Process != nil {
			_ = server.Process.Kill()
			_ = server.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get(origin + "/api/auth/fastcas/available")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("FastResearch startup timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	browser := exec.CommandContext(ctx, "node", "scripts/fastcas_browser_contract.mjs")
	browser.Dir = research
	browser.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+provider.issuer, "FASTCAS_CONTRACT_RESEARCH_ORIGIN="+origin)
	output, err := browser.CombinedOutput()
	if err != nil {
		t.Fatalf("FastResearch browser contract: %v\n%s\nproject: %s", err, output, serverLog.String())
	}
	t.Log(strings.TrimSpace(string(output)))
}
