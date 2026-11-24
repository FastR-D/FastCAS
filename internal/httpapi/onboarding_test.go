package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestClaimInviteKeepsImportedIdentity(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	const placeholder = "fr-test@fastresearch.invalid"
	const email = "legacy-owner@example.test"
	const password = "long legacy password"
	_, err := store.DB.Exec(ctx, `
		INSERT INTO identities(id,email,name,password_hash) VALUES('subject','fr-test@fastresearch.invalid','原账户姓名','');
		INSERT INTO fastresearch_imports(local_account_id,identity_id,legacy_name) VALUES('local','subject','原账户姓名');`)
	if err != nil {
		t.Fatal(err)
	}
	post := func(token, name string) (int, string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"token": token, "name": name, "password": password})
		req, _ := http.NewRequest(http.MethodPost, b.issuer+"/api/v1/claim", strings.NewReader(string(data)))
		req.Header.Set("Origin", b.issuer)
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result struct{ User core.Identity }
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return resp.StatusCode, result.User.ID
	}
	if _, err = store.IssueCredential(ctx, "admin", "recovery", placeholder); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("recovery allowed on an unclaimed identity: %v", err)
	}
	first, err := store.IssueClaim(ctx, "admin", placeholder, email)
	if err != nil {
		t.Fatal(err)
	}
	newest, err := store.IssueClaim(ctx, "admin", placeholder, email)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := post(first, "原账户姓名"); status != 400 {
		t.Fatalf("older invitation was accepted: %d", status)
	}
	if status, _ := post(newest, "另一位用户"); status != 400 {
		t.Fatalf("wrong name was accepted: %d", status)
	}
	if status, id := post(newest, " 原账户姓名 "); status != 200 || id != "subject" {
		t.Fatalf("claim failed or changed the subject: %d %q", status, id)
	}
	if status, _ := post(newest, "原账户姓名"); status != 400 {
		t.Fatalf("claim invitation was replayed: %d", status)
	}
	user, err := store.Authenticate(ctx, email, password, "claim-test")
	if err != nil || user.ID != "subject" || user.Role != "member" {
		t.Fatalf("claimed account login failed: %v", err)
	}
}

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
