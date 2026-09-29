// Package volumes holds what the Panel and the operator both need to know about a server's
// data volume.
package volumes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeAffinity copies the node affinity of the PersistentVolume bound to a PVC, so a Pod that mounts
// the same volume (the SFTP maintenance Pod, backup and restore Jobs) lands on the node that holds
// it — required for node-local/RWO storage (local-path, EBS), a no-op for storage that is not
// node-pinned. Provisioners write this on the PV, so this reads it back instead of deciding. nil when
// the PVC is not bound yet or the PV is not pinned.
func NodeAffinity(ctx context.Context, c client.Reader, namespace, pvcName string) (*corev1.Affinity, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: pvcName}, &pvc); err != nil {
		return nil, fmt.Errorf("looking up pvc: %w", err)
	}
	if pvc.Spec.VolumeName == "" {
		return nil, nil
	}
	var pv corev1.PersistentVolume
	if err := c.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
		return nil, fmt.Errorf("looking up persistentvolume %q: %w", pvc.Spec.VolumeName, err)
	}
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return nil, nil
	}
	return &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: pv.Spec.NodeAffinity.Required.DeepCopy(),
	}}, nil
}
