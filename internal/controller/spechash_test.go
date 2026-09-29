package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func TestSpecHashFollowsFilesChanged(t *testing.T) {
	gs := &gameserversv1alpha1.GameServer{ObjectMeta: metav1.ObjectMeta{Name: "mc"}}
	before := specHash(gs)
	// Pods created before the annotation existed carry a hash without it: an empty annotation must
	// not change the hash, or every running server would suddenly show pending changes.
	gs.Annotations = map[string]string{gameserversv1alpha1.FilesChangedAnnotation: ""}
	if specHash(gs) != before {
		t.Fatal("an empty annotation changed the hash")
	}
	gs.Annotations[gameserversv1alpha1.FilesChangedAnnotation] = "2026-09-28T12:00:00Z"
	changed := specHash(gs)
	if changed == before {
		t.Fatal("changing the server's files must change the hash (RestartRequired)")
	}
	gs.Annotations[gameserversv1alpha1.FilesChangedAnnotation] = "2026-09-28T12:05:00Z"
	if specHash(gs) == changed {
		t.Fatal("a second change must change the hash again")
	}
}
