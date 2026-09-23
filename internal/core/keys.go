package core

import (
	"context"
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
	"github.com/zitadel/oidc/v3/pkg/op"
)

// Keys are generated only by an explicit init command, never at server startup.
// Restarting must not silently invalidate sessions or change the signing key.
type KeyRing struct {
	Private    *rsa.PrivateKey
	KeyID      string
	Encryption [32]byte
	previous   []verificationKey
}
type signingKey struct{ ring *KeyRing }

func (k signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k signingKey) Key() any                                    { return k.ring.Private }
func (k signingKey) ID() string                                  { return k.ring.KeyID }

type publicKey struct{ ring *KeyRing }

func (k publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k publicKey) Use() string                        { return "sig" }
func (k publicKey) Key() any                           { return &k.ring.Private.PublicKey }
func (k publicKey) ID() string                         { return k.ring.KeyID }

func InitializeKeys(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	// Claim a new directory atomically, refusing all existing key material.
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}
	der := x509.MarshalPKCS1PrivateKey(private)
	encryption := make([]byte, 32)
	if _, err = rand.Read(encryption); err != nil {
		return err
	}
	for name, data := range map[string][]byte{"signing.pem": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), "encryption.key": encryption} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}

func LoadKeys(dir string) (*KeyRing, error) {
	signingFile := "signing.pem"
	if _, err := os.Stat(filepath.Join(dir, "keyring.json")); err == nil {
		signingFile = "keyring.json"
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, name := range []string{signingFile, "encryption.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("key files must not be readable by group or others")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, signingFile))
	if err != nil {
		return nil, err
	}
	var manifest keyManifest
	if signingFile == "keyring.json" {
		if err = json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		if manifest.Version != 1 {
			return nil, errors.New("unsupported key manifest version")
		}
		data = manifest.Private
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	if key.N.BitLen() < 2048 {
		return nil, errors.New("RSA key must be at least 2048 bits")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "encryption.key"))
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, errors.New("encryption key must have 32 bytes")
	}
	ring := &KeyRing{Private: key, KeyID: Hash(string(x509.MarshalPKCS1PublicKey(&key.PublicKey)))[:24]}
	for _, old := range manifest.Previous {
		pub, err := x509.ParsePKCS1PublicKey(old.Public)
		if err != nil {
			return nil, err
		}
		if pub.N.BitLen() < 2048 || old.Until.IsZero() {
			return nil, errors.New("invalid retained verification key")
		}
		ring.previous = append(ring.previous, verificationKey{pub, Hash(string(old.Public))[:24], old.Until})
	}
	copy(ring.Encryption[:], raw)
	return ring, nil
}
func (s *Store) SigningKey(context.Context) (op.SigningKey, error) {
	if s.Keys == nil {
		return nil, errors.New("signing key unavailable")
	}
	return signingKey{s.Keys}, nil
}
func (s *Store) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}
func (s *Store) KeySet(context.Context) ([]op.Key, error) {
	if s.Keys == nil {
		return nil, errors.New("signing key unavailable")
	}
	keys := []op.Key{publicKey{s.Keys}}
	for _, key := range s.Keys.previous {
		if key.until.After(time.Now()) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
