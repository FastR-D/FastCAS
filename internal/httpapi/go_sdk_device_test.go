package httpapi_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/FastR-D/FastCAS/internal/core"
	fastcas "github.com/FastR-D/FastCAS/sdk/go"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestGoSDKDeviceContract(t *testing.T) {
	store, b := setup(t)
	ctx := context.Background()
	if err := store.RegisterClient(ctx, core.Client{ID: "go-device", Name: "Go desktop", Public: true,
		Scopes: []string{"openid", "profile"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
		t.Fatal(err)
	}
	sdk, err := fastcas.New(fastcas.Config{Issuer: b.issuer, ClientID: "go-device", RedirectURI: b.issuer + "/device", AllowLoopbackHTTP: true}, &transactions{items: map[string]fastcas.Transaction{}})
	if err != nil {
		t.Fatal(err)
	}
	start, err := sdk.DeviceAuthorize(ctx, []string{"openid", "profile"})
	if err != nil || start.UserCode == "" || start.Interval != 5 {
		t.Fatalf("start: %+v %v", start, err)
	}
	_, err = sdk.PollDevice(ctx, start.DeviceCode)
	var protocol *fastcas.APIError
	if !errors.As(err, &protocol) || protocol.Code != "authorization_pending" {
		t.Fatalf("expected pending, got %v", err)
	}
	_, page := b.request("GET", "/login", nil)
	response, body := b.request("POST", "/login", url.Values{"csrf": {csrf(t, page)}, "email": {"alice@example.test"}, "password": {"correct horse battery staple"}, "action": {"login"}})
	if response.StatusCode != 303 {
		t.Fatalf("login: %d %s", response.StatusCode, body)
	}
	response, body = b.request("GET", "/device?user_code="+start.UserCode, nil)
	if response.StatusCode != 200 || !strings.Contains(string(body), "Go desktop") {
		t.Fatalf("device page: %d %s", response.StatusCode, body)
	}
	response, body = b.request("POST", "/device", url.Values{"csrf": {csrf(t, body)}, "user_code": {start.UserCode}, "decision": {"approve"}})
	if response.StatusCode != 200 {
		t.Fatalf("approval: %d %s", response.StatusCode, body)
	}
	_, err = store.DB.Exec(ctx, `UPDATE device_authorizations SET next_poll_at=now()-interval '1 second' WHERE device_code_hash=$1`, core.Hash(start.DeviceCode))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sdk.PollDevice(ctx, start.DeviceCode)
	if err != nil || result.Identity.Issuer != b.issuer || result.Identity.Subject == "" || result.AccessToken == "" {
		t.Fatalf("device result: %+v %v", result, err)
	}
	if _, err = sdk.PollDevice(ctx, start.DeviceCode); err == nil {
		t.Fatal("device code replay accepted")
	}
}
