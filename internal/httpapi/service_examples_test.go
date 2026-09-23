package httpapi_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceExamplesAgainstProvider(t *testing.T) {
	if os.Getenv("FASTCAS_SDK_CONTRACT") != "1" {
		t.Skip("requires built TypeScript and installed Python SDK examples")
	}
	store, provider := setup(t)
	const clientID, audience, scope = "example-service", "research-api", "insight:publish"
	secret, err := store.CreateServiceAccountAs(context.Background(), "admin", clientID, "Example service", []string{scope}, []string{audience}, false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(root, ".venv", "bin", "python")
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	env := append(os.Environ(), "FASTCAS_ISSUER="+provider.issuer, "FASTCAS_CLIENT_ID="+clientID,
		"FASTCAS_CLIENT_SECRET="+secret, "FASTCAS_AUDIENCE="+audience, "FASTCAS_SCOPE="+scope,
		"FASTCAS_ALLOW_LOOPBACK_HTTP=true", "PYTHONPATH="+filepath.Join(root, "sdk", "python", "src"))
	specs := []struct {
		name string
		args []string
	}{
		{"go", []string{"go", "run", "./examples/service-tokens"}},
		{"node", []string{"node", "examples/service-tokens/node.mjs"}},
		{"python", []string{python, "examples/service-tokens/python.py"}},
	}
	for _, spec := range specs {
		t.Run(spec.name, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, spec.args[0], spec.args[1:]...)
			cmd.Dir, cmd.Env = root, env
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("service example failed: %v", err)
			}
			var result struct {
				ClientID string `json:"client_id"`
				Audience string `json:"audience"`
				Scope    string `json:"scope"`
				Active   bool   `json:"active"`
			}
			if json.Unmarshal(output, &result) != nil || result.ClientID != clientID || result.Audience != audience || result.Scope != scope || !result.Active {
				t.Fatalf("invalid service example result: %s", output)
			}
		})
	}
	if _, err := store.SetServiceAccountActiveAs(ctx, "admin", clientID, false); err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		t.Run(spec.name+"/disabled", func(t *testing.T) {
			cmd := exec.CommandContext(ctx, spec.args[0], spec.args[1:]...)
			cmd.Dir, cmd.Env = root, env
			if err := cmd.Run(); err == nil {
				t.Fatal("disabled service identity still completed example")
			}
		})
	}
}
