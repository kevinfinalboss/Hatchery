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
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func newTestGameServerWithSecret(name string, state gameserversv1alpha1.GameServerState) (*gameserversv1alpha1.GameServer, *corev1.Secret) {
	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS(), UID: types.UID("uid-" + name)},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "minecraft"},
			State:   state,
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-sftp", Namespace: testOrgNS()},
		Data:       map[string][]byte{"hmac-key": []byte("0123456789abcdef0123456789abcdef")},
	}
	return gs, secret
}

func TestSFTPSessionRunningUsesSidecarMode(t *testing.T) {
	gs, secret := newTestGameServerWithSecret("gs-running", gameserversv1alpha1.GameServerStateRunning)
	srv := newTestServer(t, gs, secret)
	token := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-running/sftp-session"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sftpSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "sidecar" {
		t.Fatalf("expected mode sidecar, got %q", resp.Mode)
	}
	if resp.Password == "" {
		t.Fatal("expected a non-empty session token")
	}

	// No maintenance Pod should have been created for a Running server.
	var pod corev1.Pod
	err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: maintenancePodName("gs-running")}, &pod)
	if err == nil {
		t.Fatal("expected no maintenance pod to exist for a Running GameServer")
	}
}

func TestSFTPSessionStoppedCreatesMaintenancePod(t *testing.T) {
	gs, secret := newTestGameServerWithSecret("gs-stopped", gameserversv1alpha1.GameServerStateStopped)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "gs-stopped", Namespace: testOrgNS()},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName:  "pv-gs-stopped",
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-gs-stopped"},
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-1"},
						}},
					}},
				},
			},
		},
	}
	srv := newTestServer(t, gs, secret, pvc, pv)
	token := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-stopped/sftp-session"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sftpSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "maintenance" {
		t.Fatalf("expected mode maintenance, got %q", resp.Mode)
	}

	var pod corev1.Pod
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: maintenancePodName("gs-stopped")}, &pod); err != nil {
		t.Fatalf("expected maintenance pod to exist: %v", err)
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds <= 0 {
		t.Fatal("expected a positive ActiveDeadlineSeconds so the pod tears itself down")
	}
	if pod.Labels[gameserversv1alpha1.LabelGameServer] != "gs-stopped" {
		t.Fatalf("expected maintenance pod to carry the GameServer's selector label, got %v", pod.Labels)
	}
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		t.Fatal("expected the maintenance pod to inherit the PV's node affinity")
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("the maintenance pod must not mount a ServiceAccount token")
	}

	// Calling again should reuse the existing pod, not fail or duplicate it.
	rec = doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-stopped/sftp-session"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A maintenance Pod that already hit its ActiveDeadlineSeconds TTL is left
// behind by Kubernetes in a terminal phase with no Service endpoints behind
// it. The file manager re-resolves the SFTP target on every click, so a stale
// Pod must be replaced rather than reused — otherwise every later dial fails
// forever until someone deletes it by hand.
func TestSFTPSessionReplacesStaleMaintenancePod(t *testing.T) {
	gs, secret := newTestGameServerWithSecret("gs-stale", gameserversv1alpha1.GameServerStateStopped)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "gs-stale", Namespace: testOrgNS()},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	stale := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      maintenancePodName("gs-stale"),
			Namespace: testOrgNS(),
			Labels:    gameserversv1alpha1.GameServerLabels("gs-stale"),
		},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "sftp-agent", Image: "old"}}},
		Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "DeadlineExceeded"},
	}
	srv := newTestServer(t, gs, secret, pvc, stale)
	token := adminToken(t, srv)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-stale/sftp-session"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var pod corev1.Pod
	if err := srv.Client.Get(t.Context(), client.ObjectKey{Namespace: testOrgNS(), Name: maintenancePodName("gs-stale")}, &pod); err != nil {
		t.Fatalf("expected a fresh maintenance pod to exist: %v", err)
	}
	if pod.Status.Phase == corev1.PodFailed {
		t.Fatal("expected the stale Failed pod to have been replaced by a fresh one")
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds <= 0 {
		t.Fatal("expected the replacement pod to carry a fresh ActiveDeadlineSeconds")
	}
}

func TestSFTPSessionMissingGameServer(t *testing.T) {
	srv := newTestServer(t)
	token := adminToken(t, srv)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/does-not-exist/sftp-session"), token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSFTPSessionRequiresOrgMembership(t *testing.T) {
	gs, secret := newTestGameServerWithSecret("gs-scoped", gameserversv1alpha1.GameServerStateRunning)
	srv := newTestServer(t, gs, secret)

	outsider := newUserToken(t, srv, "not-in-org", false)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-scoped/sftp-session"), outsider, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a non-member must get 404, got %d: %s", rec.Code, rec.Body.String())
	}

	member := memberWithGrants(t, srv, "in-org", paneldb.Grant{GameServer: paneldb.AllServers, Permissions: paneldb.AllPermissions})
	rec = doRequest(t, srv, http.MethodPost, orgURL("/gameservers/gs-scoped/sftp-session"), member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a member must get 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
