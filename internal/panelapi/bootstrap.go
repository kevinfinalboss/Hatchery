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
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// BootstrapAdminUsername is the account BootstrapAdmin creates.
const BootstrapAdminUsername = "admin"

// BootstrapAdmin mirrors ArgoCD's own first-run UX: if no credentials Secret
// already exists, it generates a random password, creates (or resets) the
// "admin" user with it, and stores it in a Secret for whoever deployed the
// Panel to go fetch (`kubectl get secret <secretName> -o
// jsonpath={.data.password} | base64 -d`) — the password is never logged.
//
// The Secret's existence, not the database's, is what "already bootstrapped"
// means here: checking whether an admin user exists instead would leave no
// way to ever learn the password if the process crashed after writing the
// user but before writing the Secret. Checking the Secret first means a
// retry after exactly that crash just resets the same admin's password and
// writes the Secret it's missing, rather than silently doing nothing because
// the user row was already there.
func BootstrapAdmin(ctx context.Context, k8sClient client.Client, db *paneldb.Store, namespace, secretName string) error {
	var existing corev1.Secret
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &existing)
	if err == nil {
		return nil // already bootstrapped
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("checking for existing admin secret: %w", err)
	}

	password, err := randomPassword()
	if err != nil {
		return fmt.Errorf("generating admin password: %w", err)
	}
	if _, err := db.UpsertUser(ctx, BootstrapAdminUsername, password, true); err != nil {
		return fmt.Errorf("creating admin user: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
		StringData: map[string]string{
			"username": BootstrapAdminUsername,
			"password": password,
		},
	}
	if err := k8sClient.Create(ctx, secret); err != nil {
		return fmt.Errorf("creating admin credentials secret: %w", err)
	}
	return nil
}

func randomPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
