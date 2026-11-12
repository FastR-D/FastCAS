package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestUserManagementHTTP(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	admin, err := store.CreateIdentity(ctx, "user-admin@example.test", "User Admin", "long user admin test password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateIdentity(ctx, "managed@example.test", "Managed User", "long managed user test password", "member")
	if err != nil {
		t.Fatal(err)
	}
	token, csrfValue, err := store.NewSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any, session, origin, csrf string, expected int) []byte {
		t.Helper()
		var raw io.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.NewReader(encoded)
		}
		req, err := http.NewRequest(method, b.issuer+path, raw)
		if err != nil {
			t.Fatal(err)
		}
		if session != "" {
			req.AddCookie(&http.Cookie{Name: "fastcas_session", Value: session})
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf)
		response, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != expected {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.StatusCode, expected, data)
		}
		return data
	}
	listPath := "/api/v1/admin/identities"
	userPath := listPath + "/" + user.ID
	changes := map[string]string{"name": "Edited User", "email": "edited@example.test", "role": "member"}
	request("GET", listPath, nil, "", b.issuer, "", 401)
	request("GET", listPath, nil, token, b.issuer, csrfValue, 403)
	request("POST", userPath, changes, token, b.issuer, csrfValue, 403)
	request("POST", userPath+"/delete", map[string]any{}, token, b.issuer, csrfValue, 403)
	if _, err = store.DB.Exec(ctx, `UPDATE identities SET mfa_secret='\x01'::bytea WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE identity_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{userPath, userPath + "/delete"} {
		request("POST", path, changes, token, "https://attacker.test", csrfValue, 403)
		request("POST", path, changes, token, b.issuer, "", 403)
	}
	memberToken, memberCSRF, err := store.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	request("GET", listPath, nil, memberToken, b.issuer, memberCSRF, 403)
	request("POST", userPath, changes, memberToken, b.issuer, memberCSRF, 403)
	request("POST", userPath+"/delete", map[string]any{}, memberToken, b.issuer, memberCSRF, 403)
	data := request("GET", listPath, nil, token, b.issuer, csrfValue, 200)
	if strings.Contains(string(data), "password_hash") || strings.Contains(string(data), "mfa_secret") {
		t.Fatal("user list exposed credentials")
	}
	var listed []core.ManagedIdentity
	if err = json.Unmarshal(data, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].CreatedAt.IsZero() {
		t.Fatal("user list missing records or metadata")
	}
	request("POST", userPath, map[string]string{"name": "Conflict", "email": admin.Email, "role": "member"}, token, b.issuer, csrfValue, 409)
	data = request("POST", userPath, changes, token, b.issuer, csrfValue, 200)
	var updated core.ManagedIdentity
	if err = json.Unmarshal(data, &updated); err != nil || updated.Name != changes["name"] || updated.Email != changes["email"] {
		t.Fatal("edited profile not returned")
	}
	request("GET", "/api/v1/me", nil, memberToken, b.issuer, memberCSRF, 401)
	request("POST", listPath+"/"+admin.ID, map[string]string{"name": admin.Name, "email": admin.Email, "role": "member"}, token, b.issuer, csrfValue, 403)
	request("POST", listPath+"/"+admin.ID+"/delete", map[string]any{}, token, b.issuer, csrfValue, 403)
	request("POST", userPath+"/delete", map[string]any{}, token, b.issuer, csrfValue, 204)
	data = request("GET", listPath, nil, token, b.issuer, csrfValue, 200)
	if strings.Contains(string(data), user.ID) || strings.Contains(string(data), changes["email"]) {
		t.Fatal("deleted user remains in list")
	}
	request("POST", userPath+"/status", map[string]string{"status": "active"}, token, b.issuer, csrfValue, 403)
	request("POST", userPath, changes, token, b.issuer, csrfValue, 403)
}
