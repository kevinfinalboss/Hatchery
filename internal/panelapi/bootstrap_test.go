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

package panelapi

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testAdminSecretNamespace = "default"
	testAdminSecretName      = "panel-admin-credentials"
)

func TestBootstrapAdminCreatesUserAndSecret(t *testing.T) {
	srv := newTestServer(t)
	ctx := t.Context()

	if err := BootstrapAdmin(ctx, srv.Client, srv.DB, testAdminSecretNamespace, testAdminSecretName); err != nil {
		t.Fatal(err)
	}

	var secret corev1.Secret
	if err := srv.Client.Get(ctx, client.ObjectKey{Namespace: testAdminSecretNamespace, Name: testAdminSecretName}, &secret); err != nil {
		t.Fatalf("expected the admin secret to exist: %v", err)
	}
	if secret.StringData["username"] != BootstrapAdminUsername {
		t.Fatalf("unexpected username in secret: %q", secret.StringData["username"])
	}
	password := secret.StringData["password"]
	if password == "" {
		t.Fatal("expected a non-empty generated password")
	}

	// The generated credentials should actually work.
	if _, err := srv.DB.VerifyPassword(ctx, BootstrapAdminUsername, password); err != nil {
		t.Fatalf("expected the generated password to verify: %v", err)
	}
}

func TestBootstrapAdminIsANoOpWhenSecretAlreadyExists(t *testing.T) {
	srv := newTestServer(t)
	ctx := t.Context()

	if err := BootstrapAdmin(ctx, srv.Client, srv.DB, testAdminSecretNamespace, testAdminSecretName); err != nil {
		t.Fatal(err)
	}
	var first corev1.Secret
	if err := srv.Client.Get(ctx, client.ObjectKey{Namespace: testAdminSecretNamespace, Name: testAdminSecretName}, &first); err != nil {
		t.Fatal(err)
	}

	if err := BootstrapAdmin(ctx, srv.Client, srv.DB, testAdminSecretNamespace, testAdminSecretName); err != nil {
		t.Fatal(err)
	}
	var second corev1.Secret
	if err := srv.Client.Get(ctx, client.ObjectKey{Namespace: testAdminSecretNamespace, Name: testAdminSecretName}, &second); err != nil {
		t.Fatal(err)
	}

	if first.StringData["password"] != second.StringData["password"] {
		t.Fatal("expected the password to stay the same when the secret already existed")
	}
}

// TestBootstrapAdminRecoversFromPartialFailure simulates the process
// crashing after the admin user was written to Postgres but before the
// credentials Secret was created — the scenario BootstrapAdmin's doc comment
// says checking the Secret (not the database) for "already bootstrapped" is
// meant to recover from.
func TestBootstrapAdminRecoversFromPartialFailure(t *testing.T) {
	srv := newTestServer(t)
	ctx := t.Context()

	if _, err := srv.DB.UpsertUser(ctx, BootstrapAdminUsername, "some-password-nobody-will-ever-know", true); err != nil {
		t.Fatal(err)
	}

	if err := BootstrapAdmin(ctx, srv.Client, srv.DB, testAdminSecretNamespace, testAdminSecretName); err != nil {
		t.Fatal(err)
	}

	var secret corev1.Secret
	if err := srv.Client.Get(ctx, client.ObjectKey{Namespace: testAdminSecretNamespace, Name: testAdminSecretName}, &secret); err != nil {
		t.Fatalf("expected the admin secret to have been created on retry: %v", err)
	}
	if _, err := srv.DB.VerifyPassword(ctx, BootstrapAdminUsername, secret.StringData["password"]); err != nil {
		t.Fatalf("expected the secret's password to match the (reset) admin password: %v", err)
	}
}
