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

func TestFastTaskAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastTask and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backchannel := "http://" + listener.Addr().String() + "/api/v1/auth/fastcas/backchannel-logout"
	listener.Close()
	if err := store.RegisterClient(context.Background(), core.Client{ID: "task", Name: "FastTask", Development: true, Redirects: []string{"http://127.0.0.1:10000/api/v1/auth/fastcas/callback"}, BackchannelURL: backchannel, Scopes: []string{"openid", "profile", "email"}}, clientSecret); err != nil {
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
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/httpapi", "-run", "^TestFastCASAgainstProvider$", "-count=1", "-v")
	cmd.Dir = "../../../FastTask"
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_BACKCHANNEL="+backchannel, "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastTask contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestFastTaskIdentityStatusAgainstProvider(t *testing.T) {
	runIdentityStatusContract(t, statusContract{clientID: "task", name: "FastTask", callback: "http://127.0.0.1:10000/api/v1/auth/fastcas/callback", eventsPath: "/api/v1/auth/fastcas/events", failFirstDelivery: true, command: func(root, issuer, origin, signal string) *exec.Cmd {
		cmd := exec.Command("go", "test", "./internal/httpapi", "-run", "^TestFastCASAgainstProvider$", "-count=1", "-v")
		cmd.Dir = filepath.Join(root, "FastTask")
		cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+issuer, "FASTCAS_CONTRACT_BACKCHANNEL="+origin+"/api/v1/auth/fastcas/backchannel-logout", "FASTCAS_CONTRACT_STATUS_SIGNAL="+signal, "GOPROXY=off")
		return cmd
	}})
}
