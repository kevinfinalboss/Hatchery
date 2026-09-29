package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
	"github.com/kevinfinalboss/Hatchery/internal/twofactor"
)

const (
	// loginChallengeTTL is how long the second login step may take.
	loginChallengeTTL = 5 * time.Minute
	// loginChallengeMaxFailures wrong codes kill the challenge; the password is needed again.
	loginChallengeMaxFailures = 5
	// pendingSecretTTL is how long a setup waits for its confirming code.
	pendingSecretTTL = 10 * time.Minute
)

// twoFactorOn: TOTP needs the encryption key; without it the feature is off.
func (s *Server) twoFactorOn() bool { return s.TwoFactor != nil }

// requireTwoFactorOn answers 404 for the 2FA routes when the panel has no encryption key.
func (s *Server) requireTwoFactorOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.twoFactorOn() {
			writeError(w, http.StatusNotFound, "two-factor authentication is not enabled on this panel")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ownUserResponse is the user as seen by themselves: with the recovery codes left.
func (s *Server) ownUserResponse(ctx context.Context, u *paneldb.User) userResponse {
	resp := toUserResponse(u)
	if u.TwoFactorEnabled {
		if n, err := s.DB.CountRecoveryCodes(ctx, u.ID); err == nil {
			resp.TwoFactor.RecoveryCodesLeft = &n
		}
	}
	return resp
}

type twoFactorChallengeResponse struct {
	TwoFactorRequired bool   `json:"twoFactorRequired"`
	Ticket            string `json:"ticket"`
	ExpiresInSeconds  int    `json:"expiresInSeconds"`
}

// startTwoFactorLogin answers a right password on an account with 2FA: a challenge ticket, never a
// session. Without the encryption key or the challenge store it fails closed.
func (s *Server) startTwoFactorLogin(w http.ResponseWriter, r *http.Request, user *paneldb.User) {
	if !s.twoFactorOn() || s.LoginChallenges == nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor login is temporarily unavailable")
		return
	}
	ticket, err := s.LoginChallenges.Issue(r.Context(), user.ID, loginChallengeTTL)
	if err != nil {
		authLog.Error(err, "could not issue a two-factor login challenge")
		writeError(w, http.StatusServiceUnavailable, "two-factor login is temporarily unavailable")
		return
	}
	s.auditEventAs(r, user, "", "login.password_ok", "user", user.Username, "success", nil)
	writeJSON(w, http.StatusOK, twoFactorChallengeResponse{TwoFactorRequired: true, Ticket: ticket,
		ExpiresInSeconds: int(loginChallengeTTL.Seconds())})
}

// checkSecondFactor verifies a TOTP code (recording its step, so it works once) or consumes a
// recovery code. method is "totp" or "recovery".
func (s *Server) checkSecondFactor(ctx context.Context, userID int64, code string) (method string, ok bool, err error) {
	if twofactor.IsRecoveryCode(code) {
		ok, err := s.DB.UseRecoveryCode(ctx, userID, twofactor.HashRecoveryCode(code))
		return "recovery", ok, err
	}
	enc, enabled, err := s.DB.GetTOTP(ctx, userID)
	if err != nil || !enabled {
		return "totp", false, err
	}
	secret, err := s.TwoFactor.Decrypt(enc)
	if err != nil {
		return "totp", false, err
	}
	step, ok := twofactor.Verify(secret, code, s.clock())
	if !ok {
		return "totp", false, nil
	}
	ok, err = s.DB.AcceptTOTPStep(ctx, userID, step)
	return "totp", ok, err
}

type login2FARequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

type login2FAResponse struct {
	loginResponse
	RecoveryCodesLeft *int `json:"recoveryCodesLeft,omitempty"`
}

func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	var req login2FARequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !s.twoFactorOn() || s.LoginChallenges == nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor login is temporarily unavailable")
		return
	}
	expired := func() {
		writeErrorCode(w, http.StatusUnauthorized, "ticket_expired", "the login expired, enter your password again")
	}
	userID, err := s.LoginChallenges.Peek(r.Context(), req.Ticket)
	if errors.Is(err, panelcache.ErrChallengeNotFound) {
		expired()
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor login is temporarily unavailable")
		return
	}
	user, err := s.DB.GetUser(r.Context(), userID)
	if err != nil {
		expired()
		return
	}
	ip := clientIP(r, s.TrustedProxies)
	key := s.limiterKey(r.Context(), user.Username)
	blocked, retryAfter, err := s.LoginLimiter.Blocked(r.Context(), key, ip)
	if err != nil {
		authLog.Error(err, "login rate limiter unavailable, failing open")
	} else if blocked {
		s.auditEventAs(r, user, "", "login.blocked", "user", user.Username, "denied", nil)
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts, try again later")
		return
	}

	method, ok, err := s.checkSecondFactor(r.Context(), user.ID, req.Code)
	if err != nil {
		authLog.Error(err, "checking a second factor")
	}
	if !ok {
		if _, ferr := s.LoginChallenges.Fail(r.Context(), req.Ticket, loginChallengeMaxFailures); ferr != nil {
			authLog.Error(ferr, "could not count a failed code")
		}
		if rerr := s.LoginLimiter.RecordFailure(r.Context(), key, ip); rerr != nil {
			authLog.Error(rerr, "could not record failed login")
		}
		s.auditEventAs(r, user, "", "login.2fa_failure", "user", user.Username, "denied", nil)
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}
	// Consume closes the race of two requests with the same ticket: only one gets a session.
	if _, err := s.LoginChallenges.Consume(r.Context(), req.Ticket); err != nil {
		expired()
		return
	}
	if rerr := s.LoginLimiter.RecordSuccess(r.Context(), key, ip); rerr != nil {
		authLog.Error(rerr, "could not reset failed-login counter")
	}
	token, expiresAt, err := s.DB.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEventAs(r, user, "", "login.success", "user", user.Username, "success", map[string]string{"method": method})
	resp := login2FAResponse{loginResponse: loginResponse{Token: token, ExpiresAt: expiresAt, User: toUserResponse(user)}}
	if method == "recovery" {
		if n, err := s.DB.CountRecoveryCodes(r.Context(), user.ID); err == nil {
			resp.RecoveryCodesLeft = &n
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type setupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"`
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user.TwoFactorEnabled {
		writeError(w, http.StatusConflict, "two-factor authentication is already on; turn it off first to change devices")
		return
	}
	if s.PendingSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor setup is temporarily unavailable")
		return
	}
	secret, err := twofactor.NewSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enc, err := s.TwoFactor.Encrypt(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.PendingSecrets.Put(r.Context(), user.ID, enc, pendingSecretTTL); err != nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor setup is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, setupResponse{Secret: secret, OTPAuthURL: twofactor.OTPAuthURL(user.Username, secret)})
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if user.TwoFactorEnabled {
		writeError(w, http.StatusConflict, "two-factor authentication is already on")
		return
	}
	if s.PendingSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "two-factor setup is temporarily unavailable")
		return
	}
	enc, err := s.PendingSecrets.Get(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "no setup in progress, or it expired; start again")
		return
	}
	secret, err := s.TwoFactor.Decrypt(enc)
	if err != nil {
		writeError(w, http.StatusConflict, "the setup can no longer be read; start again")
		return
	}
	step, ok := twofactor.Verify(secret, req.Code, s.clock())
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "the code does not match; check the authenticator app and the device clock")
		return
	}
	plain, hashes, err := twofactor.NewRecoveryCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.EnableTOTP(r.Context(), user.ID, enc, step, hashes); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.PendingSecrets.Delete(r.Context(), user.ID)
	s.auditEvent(r, "", "user.2fa.enable", "user", user.Username, "success", nil)
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: plain})
}

type passwordAndCode struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// confirmIdentity checks the password and a current second factor for disable/regenerate; it
// writes 403 and returns false when either is wrong.
func (s *Server) confirmIdentity(w http.ResponseWriter, r *http.Request, user *paneldb.User) bool {
	var req passwordAndCode
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	if !user.TwoFactorEnabled {
		writeError(w, http.StatusConflict, "two-factor authentication is off")
		return false
	}
	if err := s.DB.VerifyUserPassword(r.Context(), user.ID, req.Password); err != nil {
		writeError(w, http.StatusForbidden, "wrong password or code")
		return false
	}
	_, ok, err := s.checkSecondFactor(r.Context(), user.ID, req.Code)
	if err != nil {
		authLog.Error(err, "checking a second factor")
	}
	if !ok {
		writeError(w, http.StatusForbidden, "wrong password or code")
		return false
	}
	return true
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if !s.confirmIdentity(w, r, user) {
		return
	}
	if err := s.DB.DisableTOTP(r.Context(), user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "user.2fa.disable", "user", user.Username, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handle2FARegenerateCodes(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if !s.confirmIdentity(w, r, user) {
		return
	}
	plain, hashes, err := twofactor.NewRecoveryCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.ReplaceRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "user.2fa.recovery.regenerate", "user", user.Username, "success", nil)
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: plain})
}

// handleAdminDisable2FA is the platform admin's way out for someone who lost both the authenticator
// and the recovery codes. The account's sessions are revoked: whoever is inside may be the attacker.
func (s *Server) handleAdminDisable2FA(w http.ResponseWriter, r *http.Request) {
	id, err := userIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if id == userFromContext(r.Context()).ID {
		writeError(w, http.StatusConflict, "use your own account page to turn off your two-factor authentication")
		return
	}
	target, err := s.DB.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err := s.DB.DisableTOTP(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.RevokeUserSessions(r.Context(), id, ""); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "user.2fa.admin_disable", "user", target.Username, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
