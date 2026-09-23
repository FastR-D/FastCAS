package testutil

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

var keyOnce sync.Once
var testKeys *core.KeyRing
var keyError error

func Store(t *testing.T) *core.Store {
	t.Helper()
	dsn := os.Getenv("FASTCAS_TEST_DATABASE_URL")
	if dsn == "" {
		_, file, _, _ := runtime.Caller(0)
		raw, _ := os.ReadFile(filepath.Join(filepath.Dir(file), "../../.local/test-database-url"))
		dsn = strings.TrimSpace(string(raw))
	}
	if dsn == "" {
		t.Skip("requires dedicated PostgreSQL: FASTCAS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	schema := "test_" + core.Hash(core.RandomToken())[:20]
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	keyOnce.Do(func() {
		private, err := rsa.GenerateKey(rand.Reader, 2048)
		keyError = err
		testKeys = &core.KeyRing{Private: private, KeyID: "integration-test-key"}
		_, err = rand.Read(testKeys.Encryption[:])
		if err != nil {
			keyError = err
		}
	})
	if keyError != nil {
		t.Fatal(keyError)
	}
	s := &core.Store{DB: pool, Keys: testKeys, Dev: true}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		if err != nil {
			t.Errorf("cleanup test schema: %v", err)
		}
	})
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
