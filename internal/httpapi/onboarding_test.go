package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestInviteAndRecoveryHTTPKeepAccountAndInvalidateSession(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	invite, err := store.IssueCredential(ctx, "admin", "invite", "new-user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	post := func(path string, payload map[string]string) (int, map[string]any) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", b.issuer+path, strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", b.issuer)
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return resp.StatusCode, result
	}
	status, created := post("/api/v1/register", map[string]string{"token": invite, "name": "New Member", "password": "original long password"})
	if status != 200 || created["login_required"] != true {
		t.Fatalf("invite: %d %v", status, created)
	}
	user := created["user"].(map[string]any)
	id := user["id"].(string)
	if status, _ = post("/api/v1/register", map[string]string{"token": invite, "name": "Other", "password": "original long password"}); status == 200 {
		t.Fatal("invite replay accepted")
	}
	_, page := b.request("GET", "/login", nil)
	response, _ := b.request("POST", "/login", url.Values{"csrf": {csrf(t, page)}, "email": {"new-user@example.test"}, "password": {"original long password"}, "action": {"login"}})
	if response.StatusCode != 303 {
		t.Fatalf("login: %d", response.StatusCode)
	}
	response, body := b.request("GET", "/api/v1/me", nil)
	if response.StatusCode != 200 {
		t.Fatalf("new account session: %d %s", response.StatusCode, body)
	}
	recovery, err := store.IssueCredential(ctx, "admin", "recovery", "new-user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	status, recovered := post("/api/v1/recover", map[string]string{"token": recovery, "password": "replacement long password"})
	if status != 200 || recovered["user"].(map[string]any)["id"] != id {
		t.Fatalf("recover: %d %v", status, recovered)
	}
	response, _ = b.request("GET", "/api/v1/me", nil)
	if response.StatusCode != 401 {
		t.Fatal("old browser session survived password recovery")
	}
	_, page = b.request("GET", "/login", nil)
	response, _ = b.request("POST", "/login", url.Values{"csrf": {csrf(t, page)}, "email": {"new-user@example.test"}, "password": {"original long password"}, "action": {"login"}})
	if response.StatusCode == 303 {
		t.Fatal("old password still works")
	}
	_, page = b.request("GET", "/login", nil)
	response, _ = b.request("POST", "/login", url.Values{"csrf": {csrf(t, page)}, "email": {"new-user@example.test"}, "password": {"replacement long password"}, "action": {"login"}})
	if response.StatusCode != 303 {
		t.Fatal("new password cannot log in")
	}
}
