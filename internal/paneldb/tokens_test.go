package paneldb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestResetPasswordIsSingleUseAndRevokesSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "jhon", "jhon@example.com", "password1", false)
	sess, _, _ := s.CreateSession(ctx, u.ID, time.Hour)

	tok, err := s.IssueUserToken(ctx, u.ID, PurposePasswordReset, "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetPassword(ctx, tok, "password2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifyPassword(ctx, "kevin", "password2"); err != nil {
		t.Errorf("new password: %v", err)
	}
	if _, err := s.ValidateSession(ctx, sess); !errors.Is(err, ErrNotFound) {
		t.Errorf("session survived a reset: %v", err)
	}
	if _, err := s.ResetPassword(ctx, tok, "password3"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("reused token: err = %v, want ErrInvalidToken", err)
	}
}

func TestIssuingANewTokenInvalidatesTheOld(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	old, _ := s.IssueUserToken(ctx, u.ID, PurposePasswordReset, "", time.Hour)
	if _, err := s.IssueUserToken(ctx, u.ID, PurposePasswordReset, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetPassword(ctx, old, "password2"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("old link still works: %v", err)
	}
}

func TestExpiredAndWrongPurposeTokensAreRejected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	expired, _ := s.IssueUserToken(ctx, u.ID, PurposePasswordReset, "", -time.Minute)
	if _, err := s.ResetPassword(ctx, expired, "password2"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired: %v", err)
	}
	change, _ := s.IssueUserToken(ctx, u.ID, PurposeEmailChange, "new@example.com", time.Hour)
	if _, err := s.ResetPassword(ctx, change, "password2"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("e-mail-change token used as reset: %v", err)
	}
}

func TestConcurrentResetOnlyOneWins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	tok, _ := s.IssueUserToken(ctx, u.ID, PurposePasswordReset, "", time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ResetPassword(ctx, tok, "password2"); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}
}

func TestConfirmEmailChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	tok, _ := s.IssueUserToken(ctx, u.ID, PurposeEmailChange, "New@Example.com", time.Hour)
	got, err := s.ConfirmEmailChange(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "new@example.com" {
		t.Fatalf("email = %q", got.Email)
	}

	tok2, _ := s.IssueUserToken(ctx, u.ID, PurposeEmailChange, "taken@example.com", time.Hour)
	if _, err := s.CreateUser(ctx, "other", "taken@example.com", "password1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmEmailChange(ctx, tok2); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("taken meanwhile: err = %v, want ErrEmailTaken", err)
	}
}
