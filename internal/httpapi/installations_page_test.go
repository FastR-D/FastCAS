package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestDeviceInstallationsPaginationAndSubjectBoundary(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	user, err := store.Authenticate(ctx, "alice@example.test", "correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateIdentity(ctx, "other-installations@example.test", "Other", "other correct horse battery", "member")
	if err != nil {
		t.Fatal(err)
	}
	insert := func(index int, subject string, created time.Time) string {
		t.Helper()
		id := fmt.Sprintf("install-%03d", index)
		ref := fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", index+1)
		_, err := store.DB.Exec(ctx, `INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash,created_at)
		 VALUES($1,'write',$2,$3::uuid,$4,$5,$6)`, id, subject, ref, "token-"+id, core.Hash("secret-"+id), created)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	created := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 55; i++ {
		insert(i, user.ID, created) // identical timestamps exercise the ID tie-breaker
	}
	foreign := insert(100, other.ID, created)
	_, login := b.request("GET", "/login", nil)
	response, _ := b.request("POST", "/login", url.Values{"csrf": {csrf(t, login)}, "email": {"alice@example.test"},
		"password": {"correct horse battery staple"}, "action": {"login"}})
	if response.StatusCode != 303 {
		t.Fatalf("login status %d", response.StatusCode)
	}
	page := func(before string) (int, struct {
		Installations []core.DeviceInstallation `json:"installations"`
		NextCursor    string                    `json:"next_cursor"`
	}) {
		t.Helper()
		path := "/api/v1/me/device-installations"
		if before != "" {
			path += "?before=" + url.QueryEscape(before)
		}
		response, body := b.request("GET", path, nil)
		var result struct {
			Installations []core.DeviceInstallation `json:"installations"`
			NextCursor    string                    `json:"next_cursor"`
		}
		if response.StatusCode == 200 && json.Unmarshal(body, &result) != nil {
			t.Fatalf("invalid installation page: %s", body)
		}
		return response.StatusCode, result
	}
	status, first := page("")
	if status != 200 || len(first.Installations) != 50 || first.NextCursor != first.Installations[49].ID {
		t.Fatalf("first page: status=%d count=%d cursor=%q", status, len(first.Installations), first.NextCursor)
	}
	insert(200, user.ID, time.Now().UTC()) // new head entry must not shift older pages
	status, second := page(first.NextCursor)
	if status != 200 || len(second.Installations) != 5 || second.NextCursor != "" {
		t.Fatalf("second page: status=%d count=%d cursor=%q", status, len(second.Installations), second.NextCursor)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Installations, second.Installations...) {
		if seen[item.ID] || item.ID == foreign {
			t.Fatalf("duplicate or foreign installation: %s", item.ID)
		}
		seen[item.ID] = true
	}
	if len(seen) != 55 || seen["install-200"] {
		t.Fatalf("cursor failed to preserve snapshot boundary: %d entries", len(seen))
	}
	if status, _ := page(foreign); status != 403 {
		t.Fatalf("foreign cursor status %d", status)
	}
	if status, _ := page("missing-installation"); status != 403 {
		t.Fatalf("unknown cursor status %d", status)
	}
}
