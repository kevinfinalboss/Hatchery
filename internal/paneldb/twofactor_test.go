package paneldb

import (
	"context"
	"testing"
)

func TestTwoFactorLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "alice", "alice@example.com", "password", false)

	if enc, on, err := s.GetTOTP(ctx, u.ID); err != nil || on || enc != "" {
		t.Fatalf("fresh user: %q %v %v", enc, on, err)
	}
	if err := s.EnableTOTP(ctx, u.ID, "enc-secret", 10, []string{"h1", "h2", "h3"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetUser(ctx, u.ID)
	if !got.TwoFactorEnabled {
		t.Fatal("User.TwoFactorEnabled not set")
	}
	if enc, on, _ := s.GetTOTP(ctx, u.ID); !on || enc != "enc-secret" {
		t.Fatalf("GetTOTP = %q %v", enc, on)
	}

	// The step recorded at enable time counts: the enabling code cannot log in again.
	for _, tc := range []struct {
		step int64
		want bool
	}{{10, false}, {9, false}, {11, true}, {11, false}, {13, true}} {
		ok, err := s.AcceptTOTPStep(ctx, u.ID, tc.step)
		if err != nil || ok != tc.want {
			t.Fatalf("AcceptTOTPStep(%d) = %v, %v; want %v", tc.step, ok, err, tc.want)
		}
	}

	if n, _ := s.CountRecoveryCodes(ctx, u.ID); n != 3 {
		t.Fatalf("codes = %d", n)
	}
	if ok, _ := s.UseRecoveryCode(ctx, u.ID, "h2"); !ok {
		t.Fatal("valid code refused")
	}
	if ok, _ := s.UseRecoveryCode(ctx, u.ID, "h2"); ok {
		t.Fatal("recovery code used twice")
	}
	if err := s.ReplaceRecoveryCodes(ctx, u.ID, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.UseRecoveryCode(ctx, u.ID, "h1"); ok {
		t.Fatal("old code still valid after regenerating")
	}
	if n, _ := s.CountRecoveryCodes(ctx, u.ID); n != 1 {
		t.Fatalf("codes after replace = %d", n)
	}

	if err := s.DisableTOTP(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, on, _ := s.GetTOTP(ctx, u.ID); on {
		t.Fatal("still enabled")
	}
	if n, _ := s.CountRecoveryCodes(ctx, u.ID); n != 0 {
		t.Fatalf("codes after disable = %d", n)
	}
	// Enabling again starts the replay counter over from the new step.
	if err := s.EnableTOTP(ctx, u.ID, "enc2", 5, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.AcceptTOTPStep(ctx, u.ID, 6); !ok {
		t.Fatal("step after re-enable refused")
	}
}

func TestOrgRequire2FA(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password", false)
	member, _ := s.CreateUser(ctx, "member", "member@example.com", "password", false)
	org, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_ = s.AddMember(ctx, org.ID, member.ID, RoleMember)

	if got, _ := s.GetOrgBySlug(ctx, "acme"); got.Require2FA {
		t.Fatal("require2fa defaults to true")
	}
	if err := s.SetOrgRequire2FA(ctx, org.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetOrgBySlug(ctx, "acme"); !got.Require2FA {
		t.Fatal("require2fa not read back")
	}
	all, _ := s.ListAllOrgs(ctx)
	mine, _ := s.ListOrgsForUser(ctx, member.ID)
	if len(all) != 1 || !all[0].Require2FA || len(mine) != 1 || !mine[0].Require2FA {
		t.Fatalf("org lists: %+v %+v", all, mine)
	}

	if n, _ := s.CountMembersWithout2FA(ctx, org.ID); n != 2 {
		t.Fatalf("without 2FA = %d", n)
	}
	_ = s.EnableTOTP(ctx, owner.ID, "enc", 1, nil)
	if n, _ := s.CountMembersWithout2FA(ctx, org.ID); n != 1 {
		t.Fatalf("without 2FA after enabling = %d", n)
	}
	members, _ := s.ListMembers(ctx, org.ID)
	for _, m := range members {
		if m.TwoFactorEnabled != (m.UserID == owner.ID) {
			t.Fatalf("member %s TwoFactorEnabled = %v", m.Username, m.TwoFactorEnabled)
		}
	}
	users, _ := s.ListUsers(ctx)
	for _, u := range users {
		if u.TwoFactorEnabled != (u.ID == owner.ID) {
			t.Fatalf("user %s TwoFactorEnabled = %v", u.Username, u.TwoFactorEnabled)
		}
	}
}
