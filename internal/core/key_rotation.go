package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type retainedKey struct {
	Public []byte    `json:"public_der"`
	Until  time.Time `json:"verify_until"`
}
type keyManifest struct {
	Version  int           `json:"version"`
	Private  []byte        `json:"private_pem"`
	Previous []retainedKey `json:"previous"`
}
type verificationKey struct {
	public *rsa.PublicKey
	id     string
	until  time.Time
}

func (k verificationKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k verificationKey) Use() string                        { return "sig" }
func (k verificationKey) Key() any                           { return k.public }
func (k verificationKey) ID() string                         { return k.id }

// RotateKeys is an offline operation: stop every issuer replica before calling it.
// The atomic manifest preserves encryption material and retains only public old keys.
func RotateKeys(dir string, retain time.Duration, emergency bool) error {
	if !emergency && retain < 10*time.Minute {
		return errors.New("retention must cover five-minute tokens plus clock/cache margin (minimum 10m)")
	}
	lock, err := os.OpenFile(filepath.Join(dir, "rotation.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(lock.Name())
	defer lock.Close()
	ring, err := LoadKeys(dir)
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}
	manifest := keyManifest{Version: 1, Private: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})}
	now := time.Now().UTC()
	if !emergency {
		manifest.Previous = append(manifest.Previous, retainedKey{x509.MarshalPKCS1PublicKey(&ring.Private.PublicKey), now.Add(retain)})
		for _, old := range ring.previous {
			if old.until.After(now) {
				manifest.Previous = append(manifest.Previous, retainedKey{x509.MarshalPKCS1PublicKey(old.public), old.until})
			}
		}
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".keyring-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, "keyring.json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return err
	}
	// Once the manifest is durable, the legacy private key must not remain in
	// the volume. Only old public keys are retained for the overlap window.
	if err = os.Remove(filepath.Join(dir, "signing.pem")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return d.Sync()
}
