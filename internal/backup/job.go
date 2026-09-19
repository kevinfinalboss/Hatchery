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

// Package backup builds the restic Jobs GameServerBackupController and
// GameServerRestoreController run. restic (not a bespoke tar-to-S3 script)
// gives us deduplication, encryption and incremental snapshots for free;
// every backup for a given GameServerBackup lives in the same restic
// repository, addressed by a tag derived from the CR's name rather than a
// captured snapshot ID — that sidesteps needing the controller to scrape a
// Job's logs just to learn what restic named the snapshot it created.
package backup

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// Image is the restic build every backup/restore/cleanup Job runs.
const Image = "restic/restic:0.18.1"

// Tag deterministically names the restic tag a GameServerBackup's snapshots
// carry, so a GameServerRestore never needs to know a snapshot ID — just the
// GameServerBackup's name.
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

// jobBackoffLimit is kept low: a restic failure (bad credentials, an
// unreachable bucket) is almost never transient in the way a generic
// workload's crash might be, so retrying a handful of times is about
// catching flaky networking, not masking a real config error behind delay.
var jobBackoffLimit int32 = 2

// job builds the common Job shape; the caller (GameServerBackupController or
// GameServerRestoreController) is responsible for calling
// controllerutil.SetControllerReference on the result — this package doesn't
// take a Scheme just to do that itself.
func job(name, namespace, script, pvcName string, readOnlyData bool, env []corev1.EnvVar) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &jobBackoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
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

// BackupJob builds the Job that runs `restic backup` against a GameServer's
// data volume, initializing the repository first if this is the first backup
// ever taken into it.
//
// The init step is a plain "try it, ignore failure" rather than the more
// obvious "check if it exists first, init if not": restic's S3 backend, on
// any operation against a bucket that doesn't exist yet, doesn't fail fast —
// it retries with a growing backoff for minutes before giving up. `restic
// init` itself doesn't have that problem (it creates the bucket, or fails
// immediately with "already initialized"), so running it unconditionally
// first — succeed on a fresh repo, fail fast and harmlessly on an existing
// one — sidesteps the slow path entirely instead of triggering it via a
// snapshots/exists check.
func BackupJob(bkp *gameserversv1alpha1.GameServerBackup, pvcName string) *batchv1.Job {
	const script = `set -e
restic init >/dev/null 2>&1 || true
restic backup --tag "$TAG" /data
`
	return job(bkp.Name, bkp.Namespace, script, pvcName, true, resticEnv(bkp.Spec.Destination.S3, Tag(bkp.Name)))
}

// CleanupJobName is the deterministic name of a GameServerBackup's cleanup
// Job, so the controller can look it up without recording it anywhere.
func CleanupJobName(backupName string) string {
	return backupName + "-cleanup"
}

// CleanupJob builds the Job that prunes a completed backup's snapshots from
// the remote repository. It tolerates a repository that was never
// successfully initialized (nothing to prune) rather than failing the
// finalizer forever over it. The `restic init` here exists only to dodge the
// slow-retry-on-missing-bucket path described in BackupJob's doc comment —
// this only runs after a backup reached Completed, so the bucket
// (functionally, though maybe not literally the same repository state)
// already exists in every real case.
func CleanupJob(bkp *gameserversv1alpha1.GameServerBackup) *batchv1.Job {
	const script = `restic init >/dev/null 2>&1 || true
if restic snapshots --tag "$TAG" --json 2>/dev/null | grep -q '"short_id"'; then
  restic forget --tag "$TAG" --prune
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

// RestoreJob builds the Job that runs `restic restore` for the given
// GameServerRestore, pulling the latest snapshot tagged for backupName onto
// the target GameServer's data volume. The admission webhook only allows a
// GameServerRestore whose backup already reached Completed, so the
// repository is guaranteed to exist by the time this runs; `restic init`
// still isn't needed here the way it is in BackupJob/CleanupJob.
func RestoreJob(restore *gameserversv1alpha1.GameServerRestore, dest *gameserversv1alpha1.S3Destination, backupName, pvcName string) *batchv1.Job {
	// BackupJob runs `restic backup /data`, which restic records using that
	// same absolute path inside the snapshot. `restore --target` then
	// prepends its argument to those stored paths — so target "/data" would
	// recreate the tree at "/data/data/...", not overwrite "/data" itself.
	// Target "/" reconstructs the original absolute paths, landing the
	// restored files exactly back at "/data/...".
	const script = `set -e
restic restore latest --tag "$TAG" --target /
`
	return job(restore.Name, restore.Namespace, script, pvcName, false, resticEnv(dest, Tag(backupName)))
}
