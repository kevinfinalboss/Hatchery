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

// Package backupplan decides where a GameServer's next backup goes and builds the
// GameServerBackup for it. The Panel's "Take backup" button and the operator's scheduled backups
// both use it, so both apply the same destination rules and platform limits.
package backupplan

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

const (
	// PlatformSecret is the name of the org-namespace copy of the platform's S3 credentials (see
	// Planner.ensurePlatformBackupSecret).
	PlatformSecret = "hatchery-backup-platform"
	// ConnectionSecretPrefix is prepended to a connection's name to get its Secret name.
	ConnectionSecretPrefix = "hatchery-s3-"
	// ConnectionLabel marks a Secret as one of the org's S3 connections (its value is the name).
	ConnectionLabel = "gameservers.hatchery.io/backup-connection"
	// PlatformConnection is the reserved connection name of the platform's own storage.
	PlatformConnection = "platform"
)

// ConnectionSecretName is the Secret name for a connection of the given name.
func ConnectionSecretName(name string) string { return ConnectionSecretPrefix + name }

// ConflictError is everything the planner rejects because of the caller's current state (an
// HTTP 409, in the Panel's terms) rather than a failure talking to the cluster.
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

// Platform is the platform's own backup storage, set from the panel-api flags. The Secret it
// names holds "access-key" and "secret-key". It is copied (with a per-org restic password) into
// an organization's namespace the first time that organization takes a backup there.
type Platform struct {
	Endpoint        string
	Bucket          string
	SecretNamespace string
	SecretName      string
}

// Enabled reports whether the platform offers backup storage at all.
func (p Platform) Enabled() bool { return p.Bucket != "" && p.SecretName != "" }

// Planner builds GameServerBackups for a GameServer's configured destination, applying the
// platform's per-server/per-org limits and retention when the destination is the platform's own
// storage.
type Planner struct {
	Client   client.Client
	Platform Platform
	Now      func() time.Time
}

// OrgLimits returns the org's platform-storage limits, or nil when the platform storage is off
// for it (not configured on the platform, or the org has no backups block).
func (p *Planner) OrgLimits(ctx context.Context, orgSlug string) (*v1alpha1.TenantBackupQuota, error) {
	if !p.Platform.Enabled() {
		return nil, nil
	}
	var tenant v1alpha1.Tenant
	if err := p.Client.Get(ctx, client.ObjectKey{Name: orgSlug}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return tenant.Spec.Quota.Backups, nil
}

// CountsForLimits: a failed backup holds no space worth limiting, and one already being deleted
// is on its way out.
func CountsForLimits(b *v1alpha1.GameServerBackup) bool {
	return b.Labels[v1alpha1.BackupDestinationLabel] == PlatformConnection &&
		b.Status.Phase != v1alpha1.GameServerBackupPhaseFailed && b.DeletionTimestamp.IsZero()
}

// ConnectionBuckets reads the endpoint and bucket list stored in one of an org's S3 connection
// Secrets.
func ConnectionBuckets(sec *corev1.Secret) (endpoint string, buckets []string) {
	buckets = []string{}
	_ = json.Unmarshal(sec.Data["buckets"], &buckets)
	return string(sec.Data["endpoint"]), buckets
}

// ObjectName keeps the Job name (which equals the backup's) inside the 63-character limit.
func ObjectName(server string, now time.Time) string {
	if len(server) > 40 {
		server = server[:40]
	}
	return fmt.Sprintf("%s-%s", server, strconv.FormatInt(now.Unix(), 36))
}

// RandomToken returns a URL-safe random token of n raw bytes.
func RandomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Plan works out the destination for gs's next backup (from gs.Spec.BackupTarget) and builds
// (does not create) the GameServerBackup for it, applying the platform's limits and retention
// when the destination is the platform's own storage.
func (p *Planner) Plan(ctx context.Context, gs *v1alpha1.GameServer, orgSlug string) (*v1alpha1.GameServerBackup, error) {
	ns, server := gs.Namespace, gs.Name
	target := gs.Spec.BackupTarget
	if target == nil || target.Connection == "" {
		return nil, &ConflictError{Msg: "choose where this server's backups go first"}
	}

	var s3 v1alpha1.S3Destination
	annotations := map[string]string{}
	if target.Connection == PlatformConnection {
		limits, err := p.OrgLimits(ctx, orgSlug)
		if err != nil {
			return nil, err
		}
		if limits == nil {
			return nil, &ConflictError{Msg: "the platform's backup storage is not available for this organization"}
		}
		var all v1alpha1.GameServerBackupList
		if err := p.Client.List(ctx, &all, client.InNamespace(ns)); err != nil {
			return nil, err
		}
		var perServer, perOrg int32
		for i := range all.Items {
			if !CountsForLimits(&all.Items[i]) {
				continue
			}
			perOrg++
			if all.Items[i].Labels[v1alpha1.BackupGameServerLabel] == server {
				perServer++
			}
		}
		if perServer >= limits.MaxPerServer {
			return nil, &ConflictError{Msg: fmt.Sprintf("backup limit reached: this server may keep at most %d backup(s) on the platform's storage; delete one first", limits.MaxPerServer)}
		}
		if perOrg >= limits.MaxPerOrg {
			return nil, &ConflictError{Msg: fmt.Sprintf("backup limit reached: the organization may keep at most %d backup(s) on the platform's storage; delete one first", limits.MaxPerOrg)}
		}
		if err := p.ensurePlatformBackupSecret(ctx, ns); err != nil {
			return nil, fmt.Errorf("could not prepare the platform's backup credentials: %w", err)
		}
		s3 = v1alpha1.S3Destination{
			Endpoint:  p.Platform.Endpoint,
			Bucket:    p.Platform.Bucket,
			Prefix:    orgSlug + "/" + server,
			SecretRef: corev1.LocalObjectReference{Name: PlatformSecret},
		}
		annotations[v1alpha1.BackupExpiresAtAnnotation] = p.Now().Add(time.Duration(limits.RetentionDays) * 24 * time.Hour).UTC().Format(time.RFC3339)
	} else {
		var sec corev1.Secret
		if err := p.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: ConnectionSecretName(target.Connection)}, &sec); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &ConflictError{Msg: fmt.Sprintf("the connection %q no longer exists: choose this server's backup destination again", target.Connection)}
			}
			return nil, err
		}
		endpoint, buckets := ConnectionBuckets(&sec)
		inList := false
		for _, b := range buckets {
			inList = inList || b == target.Bucket
		}
		if !inList {
			return nil, &ConflictError{Msg: fmt.Sprintf("bucket %q is no longer one of connection %q: choose this server's backup destination again", target.Bucket, target.Connection)}
		}
		prefix := target.Prefix
		if prefix == "" {
			prefix = server
		}
		s3 = v1alpha1.S3Destination{
			Endpoint:  endpoint,
			Bucket:    target.Bucket,
			Prefix:    prefix,
			SecretRef: corev1.LocalObjectReference{Name: sec.Name},
		}
	}

	bkp := &v1alpha1.GameServerBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name: ObjectName(server, p.Now()), Namespace: ns, Annotations: annotations,
			Labels: map[string]string{
				v1alpha1.BackupGameServerLabel:  server,
				v1alpha1.BackupDestinationLabel: target.Connection,
			},
		},
		Spec: v1alpha1.GameServerBackupSpec{
			GameServerRef: v1alpha1.GameServerRef{Name: server},
			Destination:   v1alpha1.BackupDestination{S3: &s3},
		},
	}
	return bkp, nil
}

// Create creates bkp, retrying once with a random suffix if two backups of the same server
// collide within the same second.
func (p *Planner) Create(ctx context.Context, bkp *v1alpha1.GameServerBackup) error {
	err := p.Client.Create(ctx, bkp)
	if apierrors.IsAlreadyExists(err) { // two backups in the same second
		suffix, _ := RandomToken(3)
		bkp.Name += "-" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(suffix))
		bkp.ResourceVersion = ""
		err = p.Client.Create(ctx, bkp)
	}
	return err
}

// ensurePlatformBackupSecret creates the org's copy of the platform's S3 credentials, with a
// restic password generated once per org. The password is never regenerated: every snapshot in
// the org's repositories is encrypted with it.
func (p *Planner) ensurePlatformBackupSecret(ctx context.Context, ns string) error {
	var src corev1.Secret
	if err := p.Client.Get(ctx, client.ObjectKey{Namespace: p.Platform.SecretNamespace, Name: p.Platform.SecretName}, &src); err != nil {
		return err
	}
	access, secret := src.Data["access-key"], src.Data["secret-key"]
	if len(access) == 0 || len(secret) == 0 {
		return fmt.Errorf("secret %s/%s needs access-key and secret-key", p.Platform.SecretNamespace, p.Platform.SecretName)
	}

	var dst corev1.Secret
	err := p.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: PlatformSecret}, &dst)
	switch {
	case apierrors.IsNotFound(err):
		password, err := RandomToken(24)
		if err != nil {
			return err
		}
		return p.Client.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: PlatformSecret, Namespace: ns},
			Data:       map[string][]byte{"access-key": access, "secret-key": secret, "restic-password": []byte(password)},
		})
	case err != nil:
		return err
	}
	if string(dst.Data["access-key"]) != string(access) || string(dst.Data["secret-key"]) != string(secret) {
		dst.Data["access-key"], dst.Data["secret-key"] = access, secret // the platform rotated its keys
		return p.Client.Update(ctx, &dst)
	}
	return nil
}
