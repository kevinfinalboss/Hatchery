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

// Package backup builds the restic Jobs for backups, restores and cleanup. A backup's snapshots are
// found by a tag derived from its name, not by snapshot ID.
package backup

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// Image is the restic build every backup/restore/cleanup Job runs.
const Image = "restic/restic:0.18.1"

// Tag is the restic tag of a GameServerBackup's snapshots.
func Tag(backupName string) string {
	return "gsbackup-" + backupName
}

func repositoryURL(dest *gameserversv1alpha1.S3Destination) string {
	endpoint := dest.Endpoint
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
	}
	repo := "s3:" + endpoint + "/" + dest.Bucket
	if dest.Prefix != "" {
		repo += "/" + dest.Prefix
	}
	return repo
}

func resticEnv(dest *gameserversv1alpha1.S3Destination, tag string) []corev1.EnvVar {
	secretRef := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: dest.SecretRef,
			Key:                  key,
		}}
	}
	return []corev1.EnvVar{
		{Name: "RESTIC_REPOSITORY", Value: repositoryURL(dest)},
		{Name: "RESTIC_PASSWORD", ValueFrom: secretRef("restic-password")},
		{Name: "AWS_ACCESS_KEY_ID", ValueFrom: secretRef("access-key")},
		{Name: "AWS_SECRET_ACCESS_KEY", ValueFrom: secretRef("secret-key")},
		{Name: "TAG", Value: tag},
	}
}

// restic failures are rarely transient: retry only a few times.
var jobBackoffLimit int32 = 2

// job builds the common Job; the caller sets the controller reference.
func job(name, namespace, script, pvcName string, readOnlyData bool, env []corev1.EnvVar) *batchv1.Job {
	// restic only talks to the S3 endpoint, never to the Kubernetes API.
	automountToken := false
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &jobBackoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: &automountToken,
					Containers: []corev1.Container{{
						Name:    "restic",
						Image:   Image,
						Command: []string{"/bin/sh", "-c", script},
						Env:     env,
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/data", ReadOnly: readOnlyData},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
						},
					}},
				},
			},
		},
	}
}

// BackupJob runs `restic backup` on the data volume. `restic init` always runs first and its
// failure is ignored: other commands against a missing bucket retry for minutes instead of failing.
// affinity pins the Job to the node holding the volume (volumes.NodeAffinity); nil lets it run anywhere.
func BackupJob(bkp *gameserversv1alpha1.GameServerBackup, pvcName string, affinity *corev1.Affinity) *batchv1.Job {
	const script = `set -e
restic init >/dev/null 2>&1 || true
restic backup --tag "$TAG" /data
`
	j := job(bkp.Name, bkp.Namespace, script, pvcName, true, resticEnv(bkp.Spec.Destination.S3, Tag(bkp.Name)))
	j.Spec.Template.Spec.Affinity = affinity
	return j
}

// CleanupJobName is the deterministic name of a GameServerBackup's cleanup
// Job, so the controller can look it up without recording it anywhere.
func CleanupJobName(backupName string) string {
	return backupName + "-cleanup"
}

// CleanupJob forgets and prunes a backup's snapshots, tolerating a repository that was never
// initialized.
func CleanupJob(bkp *gameserversv1alpha1.GameServerBackup) *batchv1.Job {
	const script = `restic init >/dev/null 2>&1 || true
ids=$(restic snapshots --tag "$TAG" --json 2>/dev/null | grep -o '"short_id":"[^"]*"' | cut -d'"' -f4)
if [ -n "$ids" ]; then
  restic forget --prune $ids
else
  echo "no snapshots found for tag $TAG, nothing to prune"
fi
`
	// No PVC needed to delete remote data; mount nothing by reusing an
	// emptyDir instead of a real data volume.
	j := job(CleanupJobName(bkp.Name), bkp.Namespace, script, "", false, resticEnv(bkp.Spec.Destination.S3, Tag(bkp.Name)))
	j.Spec.Template.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
	return j
}

// RestoreJob restores the latest snapshot of backupName onto the data volume.
func RestoreJob(restore *gameserversv1alpha1.GameServerRestore, dest *gameserversv1alpha1.S3Destination, backupName, pvcName string, affinity *corev1.Affinity) *batchv1.Job {
	// Snapshots store the absolute path /data, so the target is / (a target of /data would restore
	// into /data/data).
	const script = `set -e
restic restore latest --tag "$TAG" --target /
`
	j := job(restore.Name, restore.Namespace, script, pvcName, false, resticEnv(dest, Tag(backupName)))
	j.Spec.Template.Spec.Affinity = affinity
	return j
}
