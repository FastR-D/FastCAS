package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
)

func TestOutboxAdminEndpointsRejectAnonymousAndMember(t *testing.T) {
	_, b := setup(t)
	for _, path := range []string{"/api/v1/admin/outbox", "/api/v1/admin/outbox?state=all", "/api/v1/admin/outbox/example/attempts", "/api/v1/admin/exchange-policies"} {
		response, _ := b.request("GET", path, nil)
		if response.StatusCode != 401 {
			t.Fatalf("anonymous list %d", response.StatusCode)
		}
	}
	authorizeCode(t, b)
	response, _ := b.request("GET", "/api/v1/admin/outbox", nil)
	if response.StatusCode != 403 {
		t.Fatalf("member list %d", response.StatusCode)
	}
	response, _ = b.request("GET", "/api/v1/admin/outbox/example/attempts", nil)
	if response.StatusCode != 403 {
		t.Fatalf("member history %d", response.StatusCode)
	}
	response, _ = b.request("POST", "/api/v1/admin/outbox/example/retry", url.Values{})
	if response.StatusCode != 403 {
		t.Fatalf("unproven retry %d", response.StatusCode)
	}
}

func TestOutboxAttemptHistoryAdminEndpointOmitsPayload(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	admin, err := store.CreateIdentity(ctx, "history-admin@example.test", "Admin", "test-admin-password-long-enough", "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE identities SET mfa_secret='\x01'::bytea WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE browser_sessions SET mfa_at=now() WHERE identity_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `INSERT INTO outbox(id,client_id,payload) VALUES('history','write','{"private":"never-return-this"}'); INSERT INTO outbox_attempts(event_id,attempt,outcome,http_status,duration_ms) VALUES('history',1,'http_error',503,42)`); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", b.issuer+"/api/v1/admin/outbox/history/attempts", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "fastcas_session="+token)
	response, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || bytes.Contains(raw, []byte("never-return-this")) || strings.Contains(string(raw), "payload") {
		t.Fatalf("unsafe history response %d: %s", response.StatusCode, raw)
	}
	var result struct {
		Attempts []core.OutboxAttempt `json:"attempts"`
	}
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Attempts) != 1 || result.Attempts[0].HTTPStatus == nil || *result.Attempts[0].HTTPStatus != 503 {
		t.Fatalf("history response: %+v %v", result, err)
	}
}
