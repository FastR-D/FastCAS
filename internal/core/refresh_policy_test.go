package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

func TestRefreshRevalidatesCurrentClientPolicy(t *testing.T) {
	for _, change := range []string{"remove-profile-scope", "remove-offline-scope", "remove-refresh-grant"} {
		for _, phase := range []string{"lookup", "issuance"} {
			t.Run(change+"/"+phase, func(t *testing.T) {
				s, user, session := fixture(t)
				ctx := context.Background()
				request := authorize(t, s, session, core.RandomToken(), "openid", "profile", "offline_access")
				_, refresh, _, err := s.CreateAccessAndRefreshTokens(ctx, request, "")
				if err != nil {
					t.Fatal(err)
				}
				var captured op.RefreshTokenRequest
				if phase == "issuance" {
					captured, err = s.TokenRequestByRefreshToken(ctx, refresh)
					if err != nil {
						t.Fatal(err)
					}
				}
				client, err := s.Client(ctx, "write")
				if err != nil {
					t.Fatal(err)
				}
				grants, scopes := client.Grants, client.Scopes
				switch change {
				case "remove-offline-scope":
					scopes = []string{"openid", "profile", "email"}
				case "remove-profile-scope":
					scopes = []string{"openid", "email", "offline_access"}
				case "remove-refresh-grant":
					grants = []oidc.GrantType{oidc.GrantTypeCode}
				}
				if _, err = s.UpdateClientCapabilitiesAs(ctx, user.ID, "write", grants, scopes, client.Resources); err != nil {
					t.Fatal(err)
				}
				if phase == "lookup" {
					if _, err = s.TokenRequestByRefreshToken(ctx, refresh); err == nil {
						t.Fatal("old refresh scopes/grant survived current client policy")
					}
				} else if _, _, _, err = s.CreateAccessAndRefreshTokens(ctx, captured, refresh); err == nil {
					t.Fatal("a captured refresh request bypassed a later client policy change")
				}
				var consumed *time.Time
				if err = s.DB.QueryRow(ctx, `SELECT consumed_at FROM refresh_tokens WHERE token_hash=$1`, core.Hash(refresh)).Scan(&consumed); err != nil {
					t.Fatal(err)
				}
				if consumed != nil {
					t.Fatal("rejected refresh was consumed")
				}
				var accessCount int
				if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM access_tokens WHERE client_id='write'`).Scan(&accessCount); err != nil {
					t.Fatal(err)
				}
				if accessCount != 1 {
					t.Fatal("policy rejection still issued an access token")
				}
				if _, err = s.UpdateClientCapabilitiesAs(ctx, user.ID, "write", client.Grants, client.Scopes, client.Resources); err != nil {
					t.Fatal(err)
				}
				valid, err := s.TokenRequestByRefreshToken(ctx, refresh)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, _, err = s.CreateAccessAndRefreshTokens(ctx, valid, refresh); err != nil {
					t.Fatal("restored policy could not refresh", err)
				}
			})
		}
	}
}
