package httpapi_test

import (
	"context"
	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFastInsightAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastInsight/FastResearch and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	secret := "integration-insight-service-secret-32-characters"
	if err := store.RegisterClient(context.Background(), core.Client{ID: "insight-service", Name: "FastInsight", Scopes: []string{"insight:publish"}, Resources: []string{"research-api"}, Grants: []oidc.GrantType{oidc.GrantTypeClientCredentials}}, secret); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	listener.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := os.Getenv("FASTCAS_INSIGHT_PYTHON")
	if python == "" {
		python = filepath.Join(root, "FastCAS", ".venv", "bin", "python")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tests/service_contract.py")
	cmd.Dir = filepath.Join(root, "FastInsight")
	cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_RESEARCH_ORIGIN="+origin, "FASTCAS_CONTRACT_INSIGHT_SECRET="+secret)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastInsight contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
