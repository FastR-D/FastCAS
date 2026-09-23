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

func TestFastReadAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastRead Python dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backchannel := "http://" + listener.Addr().String() + "/api/auth/fastcas/backchannel-logout"
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{ID: "read", Name: "FastRead", Development: true, Redirects: []string{"http://127.0.0.1:18080/api/auth/fastcas/callback"}, BackchannelURL: backchannel, Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
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
		python = filepath.Join(root, "FastRead", ".venv", "bin", "python")
		if _, err := os.Stat(python); err != nil {
			python = "python3"
		}
	}
	cmd := exec.CommandContext(ctx, python, "scripts/fastcas_contract.py")
	cmd.Dir = filepath.Join(root, "FastRead")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_BACKCHANNEL="+backchannel, "PYTHONPATH="+filepath.Join(root, "FastRead/backend")+string(os.PathListSeparator)+filepath.Join(root, "FastCAS/sdk/python/src")+string(os.PathListSeparator)+os.Getenv("PYTHONPATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastRead contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestFastReadIdentityStatusAgainstProvider(t *testing.T) {
	runIdentityStatusContract(t, statusContract{clientID: "read", name: "FastRead", callback: "http://127.0.0.1:18080/api/auth/fastcas/callback", eventsPath: "/api/auth/fastcas/events", command: func(root, issuer, origin, signal string) *exec.Cmd {
		python := os.Getenv("FASTCAS_PROJECT_PYTHON")
		if python == "" {
			python = filepath.Join(root, "FastRead/.venv/bin/python")
		}
		cmd := exec.Command(python, "scripts/fastcas_contract.py")
		cmd.Dir = filepath.Join(root, "FastRead")
		cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+issuer, "FASTCAS_CONTRACT_BACKCHANNEL="+origin+"/api/auth/fastcas/backchannel-logout", "FASTCAS_CONTRACT_STATUS_SIGNAL="+signal, "PYTHONPATH="+filepath.Join(root, "FastRead/backend")+string(os.PathListSeparator)+filepath.Join(root, "FastCAS/sdk/python/src")+string(os.PathListSeparator)+os.Getenv("PYTHONPATH"))
		return cmd
	}})
}
