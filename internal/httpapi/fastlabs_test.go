package httpapi_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestFastLabsAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_PROJECT_CONTRACT") != "1" {
		t.Skip("requires FastLabs Python SDK and FASTCAS_PROJECT_CONTRACT=1")
	}
	store, b := setup(t)
	if err := store.RegisterClient(context.Background(), core.Client{ID: "fastlab-device", Name: "FastLab desktop", Public: true,
		Scopes: []string{"openid", "profile"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
		t.Fatal(err)
	}
	user, err := store.Authenticate(context.Background(), "alice@example.test", "correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := os.Getenv("FASTCAS_LABS_PYTHON")
	if python == "" {
		python = filepath.Join(root, "FastCAS", ".venv", "bin", "python")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tests/fastcas_contract.py")
	cmd.Dir = filepath.Join(root, "FastLabs")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "FastLabs"), "FASTCAS_CONTRACT_ISSUER="+b.issuer, "FASTCAS_CONTRACT_SUBJECT="+user.ID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FastLabs contract: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
