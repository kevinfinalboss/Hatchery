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
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
	"github.com/kevinfinalboss/Hatchery/pkg/authtoken"
)

const (
	sftpSessionTTL    = 5 * time.Minute
	maintenancePodTTL = 15 * time.Minute
)

type sftpSessionResponse struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// Username is fixed — the sftp-agent doesn't have real accounts, the
	// token in Password is the entire credential (see pkg/authtoken).
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expiresAt"`
	// Mode is "sidecar" when the GameServer is Running (the sftp-agent
	// already sharing its Pod) or "maintenance" when it's Stopped (an
	// on-demand Pod was created or reused for this session).
	Mode string `json:"mode"`
}

// handleSFTPSession opens (or reuses) an SFTP access point for a GameServer
// and mints a short-lived token for it. See AGENTS.md's SFTP section: this is
// the one place the Running/Stopped duality (sidecar vs. on-demand
// maintenance Pod) becomes a single API a client doesn't need to know about.
func (s *Server) handleSFTPSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}

	var secret corev1.Secret
	secretKey := client.ObjectKey{Namespace: gs.Namespace, Name: sftpagent.SecretName(gs.Name)}
	if err := s.Client.Get(r.Context(), secretKey, &secret); err != nil {
		writeError(w, statusFor(err), "sftp secret not ready yet: "+err.Error())
		return
	}
	hmacKey, ok := secret.Data["hmac-key"]
	if !ok {
		writeError(w, http.StatusInternalServerError, "sftp secret is missing its hmac-key entry")
		return
	}

	mode := "sidecar"
	if gs.Spec.State == gameserversv1alpha1.GameServerStateStopped {
		mode = "maintenance"
		if err := s.ensureMaintenancePod(r.Context(), &gs); err != nil {
			writeError(w, http.StatusInternalServerError, "creating maintenance pod: "+err.Error())
			return
		}
	}

	token, err := authtoken.Sign(hmacKey, string(gs.UID), authtoken.ScopeSFTP, sftpSessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sftpSessionResponse{
		Host:      fmt.Sprintf("%s.%s.svc.cluster.local", gs.Name, gs.Namespace),
		Port:      sftpagent.Port,
		Username:  "sftp",
		Password:  token,
		ExpiresAt: time.Now().Add(sftpSessionTTL),
		Mode:      mode,
	})
}

func maintenancePodName(gameServerName string) string {
	return gameServerName + "-sftp-maintenance"
}

// ensureMaintenancePod creates the on-demand sftp-agent Pod for a Stopped
// GameServer if one isn't already up. It carries the GameServer's own
// selector labels so the GameServerController's Service reaches it exactly
// like it would the sidecar, and an ActiveDeadlineSeconds so it tears itself
// down without needing a separate reaper process.
func (s *Server) ensureMaintenancePod(ctx context.Context, gs *gameserversv1alpha1.GameServer) error {
	name := maintenancePodName(gs.Name)
	var existing corev1.Pod
	err := s.Client.Get(ctx, client.ObjectKey{Namespace: gs.Namespace, Name: name}, &existing)
	if err == nil {
		return nil // already up; it'll tear itself down via ActiveDeadlineSeconds
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	affinity, err := s.pvcNodeAffinity(ctx, gs)
	if err != nil {
		return err
	}

	deadline := int64(maintenancePodTTL.Seconds())
	secretName := sftpagent.SecretName(gs.Name)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: gs.Namespace,
			Labels:    gameserversv1alpha1.GameServerLabels(gs.Name),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &deadline,
			Affinity:              affinity,
			SecurityContext:       &corev1.PodSecurityContext{FSGroup: sftpagent.FSGroupPtr()},
			Volumes: []corev1.Volume{
				{
					Name: sftpagent.DefaultDataVolumeName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: gs.Name},
					},
				},
				sftpagent.SecretVolume(secretName),
			},
			Containers: []corev1.Container{
				sftpagent.Container(s.SFTPAgentImage, secretName, string(gs.UID),
					sftpagent.DefaultDataVolumeName, sftpagent.DefaultDataMountPath),
			},
		},
	}
	if err := controllerutil.SetControllerReference(gs, pod, s.Client.Scheme()); err != nil {
		return err
	}
	if err := s.Client.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// pvcNodeAffinity copies the GameServer's PVC's bound PersistentVolume's node
// affinity (if any) onto a new Affinity, so the maintenance Pod lands on
// whichever node actually holds the volume — required for node-local/RWO
// storage (e.g. local-path, EBS), a no-op for anything that isn't
// node-pinned. Provisioners that set this (kind's local-path-provisioner,
// the AWS EBS CSI driver, and friends) do so on the PV itself, so this is
// just "read it back and copy it", not a scheduling decision this code makes
// on its own.
func (s *Server) pvcNodeAffinity(ctx context.Context, gs *gameserversv1alpha1.GameServer) (*corev1.Affinity, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: gs.Namespace, Name: gs.Name}, &pvc); err != nil {
		return nil, fmt.Errorf("looking up pvc: %w", err)
	}
	if pvc.Spec.VolumeName == "" {
		return nil, nil // not bound yet; let the scheduler place it freely
	}

	var pv corev1.PersistentVolume
	if err := s.Client.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
		return nil, fmt.Errorf("looking up persistentvolume %q: %w", pvc.Spec.VolumeName, err)
	}
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return nil, nil
	}
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: pv.Spec.NodeAffinity.Required.DeepCopy(),
		},
	}, nil
}
