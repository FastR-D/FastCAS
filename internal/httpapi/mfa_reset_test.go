package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestMFAResetPublicHTTPRequiresCredentialPasswordAndOrigin(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	if _, err := store.DB.Exec(ctx, `UPDATE identities SET mfa_secret='\x01'::bytea WHERE email='alice@example.test'`); err != nil {
		t.Fatal(err)
	}
	credential, err := store.IssueCredential(ctx, "admin", "mfa_reset", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	post := func(origin, password string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"token": credential, "password": password})
		req, err := http.NewRequest(http.MethodPost, b.issuer+"/api/v1/recover-mfa", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if got := post("https://attacker.example", "correct horse battery staple"); got != 403 {
		t.Fatalf("cross-origin reset status %d", got)
	}
	if got := post(b.issuer, "incorrect password"); got != 401 {
		t.Fatalf("wrong-password reset status %d", got)
	}
	if got := post(b.issuer, "correct horse battery staple"); got != 200 {
		t.Fatalf("valid MFA reset status %d", got)
	}
	if got := post(b.issuer, "correct horse battery staple"); got != 400 {
		t.Fatalf("replayed reset status %d", got)
	}
}
