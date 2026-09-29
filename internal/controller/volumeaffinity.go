package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kevinfinalboss/Hatchery/internal/volumes"
)

// volumeAffinity pins a backup or restore Job to the node holding the server's volume. Failing to
// read it is logged, not fatal: on storage that is not node-local the Job runs fine anywhere.
func volumeAffinity(ctx context.Context, c client.Reader, namespace, pvcName string) *corev1.Affinity {
	aff, err := volumes.NodeAffinity(ctx, c, namespace, pvcName)
	if err != nil {
		logf.FromContext(ctx).Error(err, "could not read the volume's node affinity; the Job may land on another node", "pvc", pvcName)
		return nil
	}
	return aff
}
