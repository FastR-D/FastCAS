package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestAuditFiltersAndPagination(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	admin, err := store.CreateIdentity(ctx, "audit-admin@example.test", "Audit admin", "administrator-long-password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	type event struct {
		ID     int64  `json:"id"`
		Actor  string `json:"actor"`
		Action string `json:"action"`
		Target string `json:"target"`
	}
	get := func(query url.Values, cookie string, status int) []event {
		t.Helper()
		req, err := http.NewRequest("GET", b.issuer+"/api/v1/admin/audit-events?"+query.Encode(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "fastcas_session", Value: cookie})
		}
		response, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("audit status %d, want %d: %s", response.StatusCode, status, raw)
		}
		if status != http.StatusOK {
			return nil
		}
		var events []event
		if err := json.Unmarshal(raw, &events); err != nil {
			t.Fatal(err)
		}
		return events
	}
	get(nil, "", http.StatusUnauthorized)
	get(nil, token, http.StatusForbidden)
	if _, err = store.DB.Exec(ctx, `UPDATE identities SET mfa_secret='\x01'::bytea WHERE id=$1;
`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE identity_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO audit_events(actor,action,target,created_at)
 SELECT 'operator','client.rotate_secret','target-app','2026-11-10T08:00:00Z'::timestamptz FROM generate_series(1,105);
 INSERT INTO audit_events(actor,action,target,created_at) VALUES
 ('other','client.rotate_secret','target-app','2026-11-10T08:00:00Z'),
 ('operator','identity.disable','target-app','2026-11-10T08:00:00Z'),
 ('operator','client.rotate_secret','other-app','2026-11-10T08:00:00Z'),
 ('operator','client.rotate_secret','target-app','2026-11-10T09:00:00Z'),
 ('operator','client.rotate_secret','target-app','2026-11-10T07:59:59Z')`); err != nil {
		t.Fatal(err)
	}
	filters := url.Values{"action": {"client.rotate_secret"}, "actor": {"operator"}, "target": {"target-app"}, "since": {"2026-11-10T16:00:00+08:00"}, "until": {"2026-11-10T17:00:00+08:00"}}
	first := get(filters, token, http.StatusOK)
	if len(first) != 100 {
		t.Fatalf("first page: %d", len(first))
	}
	seen := map[int64]bool{}
	for _, item := range first {
		if item.Actor != "operator" || item.Action != "client.rotate_secret" || item.Target != "target-app" {
			t.Fatalf("unfiltered event: %+v", item)
		}
		seen[item.ID] = true
	}
	filters.Set("before", strconv.FormatInt(first[len(first)-1].ID, 10))
	second := get(filters, token, http.StatusOK)
	if len(second) != 5 {
		t.Fatalf("second page: %d", len(second))
	}
	for _, item := range second {
		if seen[item.ID] {
			t.Fatalf("duplicate event %d", item.ID)
		}
	}
	for key, value := range map[string]string{"action": "identity.disable", "actor": "other", "target": "other-app"} {
		items := get(url.Values{key: {value}}, token, http.StatusOK)
		if len(items) != 1 {
			t.Fatalf("individual %s filter returned %d", key, len(items))
		}
	}
	if items := get(url.Values{"action": {"  client.rotate_secret  "}, "actor": {"other"}}, token, http.StatusOK); len(items) != 1 {
		t.Fatal("trimmed filter did not match")
	}
	if items := get(url.Values{"action": {"' OR 1=1 --"}}, token, http.StatusOK); len(items) != 0 {
		t.Fatal("filter was not treated as a literal value")
	}
	for _, query := range []url.Values{
		{"before": {"invalid"}}, {"before": {"0"}}, {"before": {"-1"}},
		{"action": {strings.Repeat("a", 201)}}, {"actor": {strings.Repeat("a", 201)}}, {"target": {strings.Repeat("a", 201)}},
		{"since": {"2026-11-10"}}, {"until": {"invalid"}},
		{"since": {"2026-11-10T09:00:00Z"}, "until": {"2026-11-10T08:00:00Z"}},
		{"since": {"2026-11-10T08:00:00Z"}, "until": {"2026-11-10T08:00:00Z"}},
	} {
		get(query, token, http.StatusForbidden)
	}
}
