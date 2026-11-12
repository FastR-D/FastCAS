package core_test

import (
	"context"
	"encoding/json"
	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"testing"
)

func TestProfileScopeReturnsCurrentPlatformRole(t *testing.T) {
	s := testutil.Store(t)
	ctx := context.Background()
	user, err := s.CreateIdentity(ctx, "role@example.test", "Admin", "long-role-testing-password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []bool{false, true} {
		scopes := []string{"openid"}
		if profile {
			scopes = append(scopes, "profile")
		}
		var info oidc.UserInfo
		if err = s.SetUserinfoFromRequest(ctx, &info, &core.Authorization{Subject: user.ID}, scopes); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(&info)
		var claims map[string]any
		if err = json.Unmarshal(raw, &claims); err != nil {
			t.Fatal(err)
		}
		if profile && claims["role"] != "admin" || !profile && claims["role"] != nil {
			t.Fatal(claims)
		}
	}
	if _, err = s.DB.Exec(ctx, `UPDATE identities SET role='member' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	var info oidc.UserInfo
	if err = s.SetUserinfoFromRequest(ctx, &info, &core.Authorization{Subject: user.ID}, []string{"openid", "profile"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(&info)
	var claims map[string]any
	json.Unmarshal(raw, &claims)
	if claims["role"] != "member" {
		t.Fatal(claims)
	}
}
