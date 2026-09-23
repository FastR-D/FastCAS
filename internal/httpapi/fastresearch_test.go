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

func TestFastResearchAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastResearch SDK dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{ID: "research", Name: "FastResearch", Development: true, Redirects: []string{origin + "/api/auth/fastcas/callback"}, BackchannelURL: origin + "/api/auth/fastcas/backchannel-logout", EventsURL: origin + "/api/auth/fastcas/events", Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
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
	python := os.Getenv("FASTCAS_PROJECT_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.CommandContext(ctx, python, "scripts/fastcas_contract.py")
	cmd.Dir = filepath.Join(root, "FastResearch")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_RESEARCH_ORIGIN="+origin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastResearch contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestFastResearchIdentityStatusAgainstProvider(t *testing.T) {
	runIdentityStatusContract(t, statusContract{clientID: "research", name: "FastResearch", callback: "/api/auth/fastcas/callback", eventsPath: "/api/auth/fastcas/events", command: func(root, issuer, origin, signal string) *exec.Cmd {
		python := os.Getenv("FASTCAS_PROJECT_PYTHON")
		if python == "" {
			python = "python3"
		}
		cmd := exec.Command(python, "scripts/fastcas_contract.py")
		cmd.Dir = filepath.Join(root, "FastResearch")
		cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+issuer, "FASTCAS_CONTRACT_RESEARCH_ORIGIN="+origin, "FASTCAS_CONTRACT_STATUS_SIGNAL="+signal)
		return cmd
	}})
}
