package core

import (
	"encoding/json"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"testing"
)

func TestProfileClaimsReflectCentralRole(t *testing.T) {
	for _, role := range []string{"admin", "member"} {
		info := &oidc.UserInfo{}
		setProfileClaims(info, &Identity{Name: "Research user", Role: role})
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		var claims map[string]any
		if err = json.Unmarshal(raw, &claims); err != nil {
			t.Fatal(err)
		}
		if claims["role"] != role || claims["name"] != "Research user" {
			t.Fatalf("profile claims: %s", raw)
		}
	}
}
