/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package authtoken mints and verifies short-lived, HMAC-signed session
// tokens shared between the Panel API (which mints them) and whichever
// component owns the matching per-GameServer secret (today: the sftp-agent,
// which verifies them). This is the same pattern Wings uses with the
// Pterodactyl Panel — see AGENTS.md, "Como o Wings autentica com o Panel":
// a secret shared out of band lets the verifying side validate a session
// locally, with no round trip back to the API that minted it.
package authtoken

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Scope identifies what a token may be used for. There's only one today;
// this mirrors Wings' JwtScope so adding a second use case (e.g. a
// console-specific token, replacing the Panel API's current all-purpose
// bearer auth) doesn't require reshaping the claim type.
type Scope string

// ScopeSFTP is the only scope minted so far, for handing a browser/CLI client
// off to an sftp-agent session.
const ScopeSFTP Scope = "sftp"

// Claims is the payload of a session token.
type Claims struct {
	jwt.RegisteredClaims
	ServerUUID string `json:"server_uuid"`
	Scope      Scope  `json:"scope"`
}

// Sign mints a token scoped to serverUUID and scope, valid for ttl, signed
// with secret (the per-GameServer HMAC key).
func Sign(secret []byte, serverUUID string, scope Scope, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		ServerUUID: serverUUID,
		Scope:      scope,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// Verify parses and validates tokenString against secret, and checks it was
// actually issued for wantServerUUID and wantScope — not just any token
// signed with the right key. It never contacts the Panel API; whoever holds
// the per-GameServer secret validates sessions entirely on their own.
func Verify(secret []byte, tokenString, wantServerUUID string, wantScope Scope) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("token is not valid")
	}
	if claims.ServerUUID != wantServerUUID {
		return nil, errors.New("token was not issued for this server")
	}
	if claims.Scope != wantScope {
		return nil, errors.New("token scope does not match")
	}
	return claims, nil
}
