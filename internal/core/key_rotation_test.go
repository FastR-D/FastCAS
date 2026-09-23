package core

import (
	"context"
	jose "github.com/go-jose/go-jose/v4"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfflineSigningRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := InitializeKeys(dir); err != nil {
		t.Fatal(err)
	}
	first, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: first.Private}, nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign([]byte("existing token"))
	if err != nil {
		t.Fatal(err)
	}
	if err = RotateKeys(dir, time.Minute, false); err == nil {
		t.Fatal("unsafe retention accepted")
	}
	if err = RotateKeys(dir, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "signing.pem")); !os.IsNotExist(err) {
		t.Fatalf("legacy private key retained after rotation: %v", err)
	}
	second, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.KeyID == second.KeyID || first.Encryption != second.Encryption {
		t.Fatal("rotation changed wrong key")
	}
	keys, err := (&Store{Keys: second}).KeySet(context.Background())
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys %d: %v", len(keys), err)
	}
	if _, err = signed.Verify(keys[1].Key()); err != nil {
		t.Fatal("old token lost verification", err)
	}
	if _, err = signed.Verify(keys[0].Key()); err == nil {
		t.Fatal("old token accepted by new key")
	}
	if err = RotateKeys(dir, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	third, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ = (&Store{Keys: third}).KeySet(context.Background())
	if len(keys) != 3 {
		t.Fatal("successive rotation lost old key")
	}
	third.previous[0].until = time.Now().Add(-time.Second)
	keys, _ = (&Store{Keys: third}).KeySet(context.Background())
	if len(keys) != 2 {
		t.Fatal("expired key published")
	}
	if err = RotateKeys(dir, 0, true); err != nil {
		t.Fatal(err)
	}
	emergency, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ = (&Store{Keys: emergency}).KeySet(context.Background())
	if len(keys) != 1 || emergency.Encryption != first.Encryption {
		t.Fatal("emergency retained key or changed encryption")
	}
	if err = os.Chmod(filepath.Join(dir, "keyring.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadKeys(dir); err == nil {
		t.Fatal("readable manifest accepted")
	}
}
func TestRotationRefusesConcurrentWriterAndCorruptManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := InitializeKeys(dir); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "rotation.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RotateKeys(dir, time.Hour, false); err == nil {
		t.Fatal("concurrent writer accepted")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keyring.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeys(dir); err == nil {
		t.Fatal("fell back to old key")
	}
}
