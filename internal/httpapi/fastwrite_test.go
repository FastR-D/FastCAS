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

func TestFastWriteAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires installed FastWrite dependencies and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	for _, email := range []string{"new@example.test", "interrupted@example.test"} {
		if _, err := store.CreateIdentity(context.Background(), email, "New CAS user", "correct horse battery staple", "member"); err != nil {
			t.Fatal(err)
		}
	}
	_, err = store.DB.Exec(context.Background(), `UPDATE applications SET config=jsonb_set(config,'{redirect_uris}','["http://127.0.0.1:3003/api/auth/fastcas/callback"]'::jsonb) || jsonb_build_object('events_uri',$1::text,'backchannel_logout_uri',$2::text) WHERE id='write'`, origin+"/api/auth/fastcas/events", origin+"/api/auth/fastcas/backchannel-logout")
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
	cmd := exec.CommandContext(ctx, "bun", "../../../FastWrite/scripts/fastcas-contract.ts")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_BACKCHANNEL_ORIGIN="+origin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastWrite contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestFastWriteIdentityStatusAgainstProvider(t *testing.T) {
	runIdentityStatusContract(t, statusContract{clientID: "write", name: "FastWrite", callback: "http://127.0.0.1:3003/api/auth/fastcas/callback", eventsPath: "/api/auth/fastcas/events", command: func(root, issuer, origin, signal string) *exec.Cmd {
		cmd := exec.Command("bun", "scripts/fastcas-contract.ts")
		cmd.Dir = filepath.Join(root, "FastWrite")
		cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+issuer, "FASTCAS_CONTRACT_BACKCHANNEL_ORIGIN="+origin, "FASTCAS_CONTRACT_STATUS_SIGNAL="+signal)
		return cmd
	}})
}
