package volumes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeAffinity(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	selector := &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{
		{Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-a"}},
	}}}}
	pinned := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-local"},
		Spec: corev1.PersistentVolumeSpec{NodeAffinity: &corev1.VolumeNodeAffinity{Required: selector}}}
	free := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-net"}}
	pvc := func(name, volume string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: volume}}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pinned, free,
		pvc("local", "pv-local"), pvc("net", "pv-net"), pvc("unbound", "")).Build()
	ctx := context.Background()

	aff, err := NodeAffinity(ctx, c, "ns", "local")
	if err != nil || aff == nil || aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values[0] != "node-a" {
		t.Fatalf("pinned volume = %+v, %v", aff, err)
	}
	// Copy, not alias: changing the result must not touch the PV's own selector.
	aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values[0] = "x"
	if selector.NodeSelectorTerms[0].MatchExpressions[0].Values[0] != "node-a" {
		t.Fatal("the PV's selector was aliased")
	}
	for _, name := range []string{"net", "unbound"} {
		if aff, err := NodeAffinity(ctx, c, "ns", name); err != nil || aff != nil {
			t.Fatalf("%s = %+v, %v (want no affinity)", name, aff, err)
		}
	}
	if _, err := NodeAffinity(ctx, c, "ns", "missing"); err == nil {
		t.Fatal("missing PVC must be an error")
	}
}
