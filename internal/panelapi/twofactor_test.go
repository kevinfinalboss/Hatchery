package panelapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/twofactor"
)

func testCipher(t *testing.T) *twofactor.Cipher {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	c, err := twofactor.NewCipher(base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pinnedClock makes the server's clock controllable; it starts in the middle of a 30s step.
func pinnedClock(srv *Server) *time.Time {
	now := time.Unix(1_800_000_015, 0)
	srv.now = func() time.Time { return now }
	return &now
}

func totpAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return v
}

type setupResp struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"`
}

type challengeResp struct {
	TwoFactorRequired bool   `json:"twoFactorRequired"`
	Ticket            string `json:"ticket"`
	Token             string `json:"token"`
	RecoveryCodesLeft *int   `json:"recoveryCodesLeft"`
}

// enable2FA turns 2FA on for the session's user and returns the secret and recovery codes.
func enable2FA(t *testing.T, srv *Server, token string, now time.Time) (string, []string) {
	t.Helper()
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/setup", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup = %d %s", rec.Code, rec.Body)
	}
	secret := decodeJSON[setupResp](t, rec.Body.Bytes()).Secret
	rec = doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/enable", token, map[string]string{"code": totpAt(t, secret, now)})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable = %d %s", rec.Code, rec.Body)
	}
	codes := decodeJSON[struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}](t, rec.Body.Bytes()).RecoveryCodes
	return secret, codes
}

func loginPassword(t *testing.T, srv *Server, username, password string) (int, challengeResp) {
	t.Helper()
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": username, "password": password})
	return rec.Code, decodeJSON[challengeResp](t, rec.Body.Bytes())
}

func loginCode(t *testing.T, srv *Server, ticket, code string) (int, challengeResp, string) {
	t.Helper()
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/auth/login/2fa", "", map[string]string{"ticket": ticket, "code": code})
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	return rec.Code, decodeJSON[challengeResp](t, rec.Body.Bytes()), e.Code
}

func TestTwoFactorSetupAndLogin(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	token := newUserToken(t, srv, "alice", false)

	// Without 2FA the login is unchanged.
	if code, r := loginPassword(t, srv, "alice", "password"); code != http.StatusOK || r.Token == "" || r.TwoFactorRequired {
		t.Fatalf("login without 2FA = %d %+v", code, r)
	}

	// A wrong confirmation code does not turn 2FA on.
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/setup", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup = %d", rec.Code)
	}
	s := decodeJSON[setupResp](t, rec.Body.Bytes())
	if s.OTPAuthURL == "" || s.Secret == "" {
		t.Fatalf("setup response %+v", s)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/enable", token, map[string]string{"code": "000000"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("enable with wrong code = %d", rec.Code)
	}

	secret, codes := enable2FA(t, srv, token, *now)
	if len(codes) != 10 {
		t.Fatalf("got %d recovery codes", len(codes))
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/setup", token, nil); rec.Code != http.StatusConflict {
		t.Fatalf("setup with 2FA on = %d", rec.Code)
	}
	me := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil)
	var meBody struct {
		TwoFactor struct {
			Enabled           bool `json:"enabled"`
			RecoveryCodesLeft int  `json:"recoveryCodesLeft"`
		} `json:"twoFactor"`
	}
	_ = json.Unmarshal(me.Body.Bytes(), &meBody)
	if !meBody.TwoFactor.Enabled || meBody.TwoFactor.RecoveryCodesLeft != 10 {
		t.Fatalf("me = %s", me.Body)
	}

	// Password alone gives a ticket, never a session.
	code, r := loginPassword(t, srv, "alice", "password")
	if code != http.StatusOK || !r.TwoFactorRequired || r.Ticket == "" || r.Token != "" {
		t.Fatalf("password step = %d %+v", code, r)
	}
	// The code that enabled 2FA was already used: replay.
	if c, _, _ := loginCode(t, srv, r.Ticket, totpAt(t, secret, *now)); c != http.StatusUnauthorized {
		t.Fatalf("replayed code = %d", c)
	}
	*now = now.Add(30 * time.Second)
	c, done, _ := loginCode(t, srv, r.Ticket, totpAt(t, secret, *now))
	if c != http.StatusOK || done.Token == "" {
		t.Fatalf("totp step = %d %+v", c, done)
	}
	if c, _, ec := loginCode(t, srv, r.Ticket, totpAt(t, secret, now.Add(30*time.Second))); c != http.StatusUnauthorized || ec != "ticket_expired" {
		t.Fatalf("reused ticket = %d %s", c, ec)
	}

	// A recovery code works once and the answer says how many are left.
	_, r = loginPassword(t, srv, "alice", "password")
	c, done, _ = loginCode(t, srv, r.Ticket, codes[0])
	if c != http.StatusOK || done.Token == "" || done.RecoveryCodesLeft == nil || *done.RecoveryCodesLeft != 9 {
		t.Fatalf("recovery step = %d %+v", c, done)
	}
	_, r = loginPassword(t, srv, "alice", "password")
	if c, _, _ := loginCode(t, srv, r.Ticket, codes[0]); c != http.StatusUnauthorized {
		t.Fatalf("reused recovery code = %d", c)
	}
}

func TestTwoFactorTicketDiesAndFailuresAreRateLimited(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	token := newUserToken(t, srv, "bob", false)
	secret, _ := enable2FA(t, srv, token, *now)
	*now = now.Add(time.Minute)

	_, r := loginPassword(t, srv, "bob", "password")
	for i := 0; i < 5; i++ {
		if c, _, _ := loginCode(t, srv, r.Ticket, "000000"); c != http.StatusUnauthorized {
			t.Fatalf("wrong code %d = %d", i, c)
		}
	}
	// The ticket died: even the right code is refused.
	if c, _, ec := loginCode(t, srv, r.Ticket, totpAt(t, secret, *now)); c != http.StatusUnauthorized || ec != "ticket_expired" {
		t.Fatalf("right code on dead ticket = %d %s", c, ec)
	}
	// And the failures counted against the login limiter (5 per username+IP).
	if code, _ := loginPassword(t, srv, "bob", "password"); code != http.StatusTooManyRequests {
		t.Fatalf("password after 5 code failures = %d, want 429", code)
	}
}

func TestPasswordSuccessDoesNotResetCodeFailures(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	token := newUserToken(t, srv, "carol", false)
	_, _ = enable2FA(t, srv, token, *now)

	// Four wrong codes, a fresh password step, one more wrong code: the limiter must now block.
	_, r := loginPassword(t, srv, "carol", "password")
	for i := 0; i < 4; i++ {
		loginCode(t, srv, r.Ticket, "000000")
	}
	_, r = loginPassword(t, srv, "carol", "password")
	loginCode(t, srv, r.Ticket, "000000")
	if code, _ := loginPassword(t, srv, "carol", "password"); code != http.StatusTooManyRequests {
		t.Fatalf("interleaving password steps reset the limiter: %d", code)
	}
}

func TestTwoFactorFailsClosed(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	token := newUserToken(t, srv, "dave", false)
	_, _ = enable2FA(t, srv, token, *now)
	_ = newUserToken(t, srv, "erin", false)

	srv.LoginChallenges = nil // Valkey down
	if code, r := loginPassword(t, srv, "dave", "password"); code != http.StatusServiceUnavailable || r.Token != "" {
		t.Fatalf("2FA login without challenge store = %d %+v", code, r)
	}
	if code, r := loginPassword(t, srv, "erin", "password"); code != http.StatusOK || r.Token == "" {
		t.Fatalf("non-2FA login without challenge store = %d", code)
	}

	srv.LoginChallenges = panelcache.NewMemoryLoginChallengeStore()
	srv.TwoFactor = nil // no encryption key
	if code, _ := loginPassword(t, srv, "dave", "password"); code != http.StatusServiceUnavailable {
		t.Fatalf("2FA login without key = %d", code)
	}
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/setup", token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("setup without key = %d", rec.Code)
	}
}

func TestTwoFactorDisableAndRegenerate(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	token := newUserToken(t, srv, "frank", false)
	secret, codes := enable2FA(t, srv, token, *now)
	*now = now.Add(30 * time.Second)

	post := func(path, password, code string) int {
		return doRequest(t, srv, http.MethodPost, path, token, map[string]string{"password": password, "code": code}).Code
	}
	if c := post("/api/v1/me/2fa/recovery-codes", "wrong", totpAt(t, secret, *now)); c != http.StatusForbidden {
		t.Fatalf("regenerate with wrong password = %d", c)
	}
	if c := post("/api/v1/me/2fa/recovery-codes", "password", "000000"); c != http.StatusForbidden {
		t.Fatalf("regenerate with wrong code = %d", c)
	}
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/me/2fa/recovery-codes", token,
		map[string]string{"password": "password", "code": totpAt(t, secret, *now)})
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate = %d %s", rec.Code, rec.Body)
	}
	fresh := decodeJSON[struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}](t, rec.Body.Bytes()).RecoveryCodes
	if len(fresh) != 10 || fresh[0] == codes[0] {
		t.Fatalf("regenerated codes %v", fresh)
	}
	// An old code no longer disables 2FA; a new one does.
	if c := post("/api/v1/me/2fa/disable", "password", codes[1]); c != http.StatusForbidden {
		t.Fatalf("disable with an old recovery code = %d", c)
	}
	if c := post("/api/v1/me/2fa/disable", "password", fresh[0]); c != http.StatusNoContent {
		t.Fatalf("disable = %d", c)
	}
	if code, r := loginPassword(t, srv, "frank", "password"); code != http.StatusOK || r.Token == "" {
		t.Fatalf("login after disable = %d %+v", code, r)
	}
}

func TestAdminDisablesAnotherUsers2FA(t *testing.T) {
	srv := newTestServer(t)
	now := pinnedClock(srv)
	admin := newUserToken(t, srv, "root", true)
	token := newUserToken(t, srv, "gina", false)
	_, _ = enable2FA(t, srv, token, *now)
	gina, _ := srv.DB.GetUserByUsername(context.Background(), "gina")
	root, _ := srv.DB.GetUserByUsername(context.Background(), "root")

	if rec := doRequest(t, srv, http.MethodDelete, "/api/v1/users/"+itoa(gina.ID)+"/2fa", token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin = %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, "/api/v1/users/"+itoa(root.ID)+"/2fa", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("own account = %d", rec.Code)
	}
	if rec := doRequest(t, srv, http.MethodDelete, "/api/v1/users/"+itoa(gina.ID)+"/2fa", admin, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("admin disable = %d %s", rec.Code, rec.Body)
	}
	if u, _ := srv.DB.GetUser(context.Background(), gina.ID); u.TwoFactorEnabled {
		t.Fatal("2FA still on")
	}
	// Her sessions were revoked: whoever was inside may be the attacker.
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session after admin disable = %d", rec.Code)
	}
	events, _ := srv.DB.ListAudit(context.Background(), "", 50, 0)
	found := false
	for _, e := range events {
		found = found || e.Action == "user.2fa.admin_disable"
	}
	if !found {
		t.Fatal("no user.2fa.admin_disable audit event")
	}
}
