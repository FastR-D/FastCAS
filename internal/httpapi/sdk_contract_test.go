package httpapi_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runSDKContract(t *testing.T, command string, args []string, directory string, extraEnv ...string) {
	t.Helper()
	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("requires %s and isolated SDK dependencies: %v", command, err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	_, b := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = filepath.Join(root, directory)
	cmd.Env = append(os.Environ(), append([]string{
		"FASTCAS_CONTRACT_ISSUER=" + b.issuer,
	}, extraEnv...)...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Run(); err != nil {
		t.Fatalf("%s SDK contract: %v\n%s", directory, err, output.String())
	}
	if !strings.Contains(output.String(), "sid contract passed") {
		t.Fatalf("%s SDK contract did not report success:\n%s", directory, output.String())
	}
}

func TestTypeScriptSDKRealProviderSIDContract(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "sdk/typescript/dist/server.js")); err != nil {
		t.Skipf("requires built TypeScript SDK: %v", err)
	}
	runSDKContract(t, "node", []string{"test/sid_contract.mjs"}, "sdk/typescript")
}

func TestPythonSDKRealProviderSIDContract(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pythonPath := filepath.Join(root, "sdk/python/src")
	python := os.Getenv("FASTCAS_PYTHON_CONTRACT")
	if python == "" {
		python = "python"
	}
	runSDKContract(t, python, []string{"tests/sid_contract.py"}, "sdk/python", "PYTHONPATH="+pythonPath)
}
