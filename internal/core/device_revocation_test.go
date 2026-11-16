package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func TestIdentityRevocationInvalidatesUnclaimedDeviceApprovals(t *testing.T) {
	for _, action := range []string{"disable-and-enable", "logout-all"} {
		t.Run(action, func(t *testing.T) {
			s, user, _ := fixture(t)
			ctx := context.Background()
			if err := s.RegisterClient(ctx, core.Client{ID: "device", Name: "Device", Public: true, Scopes: []string{"openid"}, Grants: []oidc.GrantType{oidc.GrantTypeDeviceCode}}, ""); err != nil {
				t.Fatal(err)
			}
			other, err := s.CreateIdentity(ctx, "other-device@example.test", "Other", "a different long password", "member")
			if err != nil {
				t.Fatal(err)
			}
			approve := func(subject, device, code string) {
				t.Helper()
				if err := s.StoreDeviceAuthorization(ctx, "device", device, code, time.Now().Add(5*time.Minute), []string{"openid"}); err != nil {
					t.Fatal(err)
				}
				if err := s.ConfirmDevice(ctx, code, subject, true); err != nil {
					t.Fatal(err)
				}
			}
			approve(user.ID, "old-approved-device", "OLD-CODE")
			approve(other.ID, "independent-device", "OTHER-CODE")
			if action == "disable-and-enable" {
				if err := s.SetIdentityStatus(ctx, user.ID, user.ID, "disabled"); err != nil {
					t.Fatal(err)
				}
				if err := s.SetIdentityStatus(ctx, user.ID, user.ID, "active"); err != nil {
					t.Fatal(err)
				}
			} else if err := s.LogoutAll(ctx, user.ID); err != nil {
				t.Fatal(err)
			}
			state, err := s.GetDeviceAuthorizatonState(ctx, "device", "old-approved-device")
			if err == nil && state.Done {
				t.Fatal("old device approval survived identity credential revocation")
			}
			if err != nil {
				t.Fatal(err)
			}
			if !state.Denied {
				t.Fatal("revoked approval was not denied")
			}
			state, err = s.GetDeviceAuthorizatonState(ctx, "device", "independent-device")
			if err != nil || !state.Done || state.Subject != other.ID {
				t.Fatalf("another identity's approval was affected: %v", err)
			}
			approve(user.ID, "new-approved-device", "NEW-CODE")
			state, err = s.GetDeviceAuthorizatonState(ctx, "device", "new-approved-device")
			if err != nil || !state.Done || state.Subject != user.ID {
				t.Fatalf("new approval could not complete: %v", err)
			}
		})
	}
}
