package httpapi_test

import (
	"bytes"
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

func TestFastNewsIdentityStatusAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastNews dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err = store.RegisterClient(context.Background(), core.Client{ID: "news", Name: "FastNews", Development: true, Redirects: []string{origin + "/api/auth/fastcas/callback"}, EventsURL: origin + "/api/auth/fastcas/events", Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	python := filepath.Join(root, "FastNews/.venv/bin/python")
	cmd := exec.CommandContext(ctx, python, "tests/fastcas_contract.py")
	cmd.Dir = filepath.Join(root, "FastNews")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "FastNews"), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_NEWS_ORIGIN="+origin, "FASTCAS_CONTRACT_STATUS_SIGNAL="+signal)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := false
	for !ready {
		if _, err = os.Stat(signal); err == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("FastNews status contract timed out: %s", output.String())
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
	if err = cmd.Wait(); err != nil {
		t.Fatalf("FastNews status contract: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "identity status contract passed") {
		t.Fatalf("FastNews did not confirm status event: %s", output.String())
	}
	_, elapsed := identityStatusDeliveryLatency(t, store, ctx, "news")
	if elapsed > 30 {
		t.Fatalf("FastNews status event reached project after %.2fs", elapsed)
	}
	t.Logf("FastNews signed status event reached project in %.2fs", elapsed)
}

func TestFastNewsAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastNews SDK dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{ID: "news", Name: "FastNews", Development: true, Redirects: []string{origin + "/api/auth/fastcas/callback"}, BackchannelURL: origin + "/api/auth/fastcas/backchannel-logout", EventsURL: origin + "/api/auth/fastcas/events", Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
		t.Fatal(err)
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
	python := os.Getenv("FASTCAS_NEWS_PYTHON")
	if python == "" {
		python = filepath.Join(root, "FastNews/.venv/bin/python")
	}
	cmd := exec.CommandContext(ctx, python, "tests/fastcas_contract.py")
	cmd.Dir = filepath.Join(root, "FastNews")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "FastNews"), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_NEWS_ORIGIN="+origin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastNews contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
