package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEggNamespace(t *testing.T) {
	gs := &GameServer{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "hatchery-acme"}}

	if got := gs.EggNamespace(); got != "hatchery-acme" {
		t.Errorf("unset scope must mean the server's own namespace, got %q", got)
	}
	gs.Spec.EggRef.Scope = EggScopeNamespace
	if got := gs.EggNamespace(); got != "hatchery-acme" {
		t.Errorf("Namespace scope: got %q", got)
	}
	gs.Spec.EggRef.Scope = EggScopeCatalog
	if got := gs.EggNamespace(); got != CatalogNamespace {
		t.Errorf("Catalog scope must resolve to %q, got %q", CatalogNamespace, got)
	}
}
