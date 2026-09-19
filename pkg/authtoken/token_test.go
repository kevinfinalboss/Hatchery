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

package authtoken

import (
	"testing"
	"time"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	secret := []byte("super-secret-key")
	token, err := Sign(secret, "server-123", ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := Verify(secret, token, "server-123", ScopeSFTP)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims.ServerUUID != "server-123" {
		t.Fatalf("unexpected server uuid: %q", claims.ServerUUID)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token, err := Sign([]byte("secret-a"), "server-123", ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify([]byte("secret-b"), token, "server-123", ScopeSFTP); err == nil {
		t.Fatal("expected error verifying with the wrong secret")
	}
}

func TestVerifyRejectsWrongServer(t *testing.T) {
	secret := []byte("super-secret-key")
	token, err := Sign(secret, "server-123", ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(secret, token, "server-456", ScopeSFTP); err == nil {
		t.Fatal("expected error verifying a token minted for a different server")
	}
}

func TestVerifyRejectsWrongScope(t *testing.T) {
	secret := []byte("super-secret-key")
	token, err := Sign(secret, "server-123", ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(secret, token, "server-123", Scope("console")); err == nil {
		t.Fatal("expected error verifying a token against the wrong scope")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	secret := []byte("super-secret-key")
	token, err := Sign(secret, "server-123", ScopeSFTP, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(secret, token, "server-123", ScopeSFTP); err == nil {
		t.Fatal("expected error verifying an expired token")
	}
}
