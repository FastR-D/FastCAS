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
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestFastNewsResearchDelegationAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastNews, FastResearch and their local SDK dependencies")
	}
	store, b := setup(t)
	port := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		return "http://" + address
	}
	newsOrigin, researchOrigin := port(), port()
	ctx := context.Background()
	if err := store.RegisterClient(ctx, core.Client{ID: "news", Name: "FastNews", Development: true,
		Redirects: []string{newsOrigin + "/api/auth/fastcas/callback"},
		Scopes:    []string{"openid", "profile", "email", "offline_access", "research:read"},
		Resources: []string{"research-api"},
		Grants:    []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken, oidc.GrantTypeTokenExchange}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterClient(ctx, core.Client{ID: "research", Name: "FastResearch", Development: true,
		Redirects: []string{researchOrigin + "/api/auth/fastcas/callback"},
		Scopes:    []string{"openid", "profile", "email", "research:read"}}, clientSecret); err != nil {
		t.Fatal(err)
	}
	if err := store.SetExchangePolicy(ctx, "admin", "news", "research", "research-api", "research:read", true); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	contractCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	python := filepath.Join(root, "FastNews/.venv/bin/python")
	cmd := exec.CommandContext(contractCtx, python, "tests/research_delegation_contract.py")
	cmd.Dir = filepath.Join(root, "FastNews")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "FastNews"), "FASTCAS_CONTRACT_ISSUER="+b.issuer,
		"FASTCAS_CONTRACT_NEWS_ORIGIN="+newsOrigin, "FASTCAS_CONTRACT_RESEARCH_ORIGIN="+researchOrigin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("News/Research delegation contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
