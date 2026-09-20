package backup

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func TestBackupJobDoesNotMountServiceAccountToken(t *testing.T) {
	bkp := &gameserversv1alpha1.GameServerBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "hatchery-acme"},
		Spec: gameserversv1alpha1.GameServerBackupSpec{Destination: gameserversv1alpha1.BackupDestination{
			S3: &gameserversv1alpha1.S3Destination{Bucket: "bkt"},
		}},
	}
	j := BackupJob(bkp, "pvc")
	tok := j.Spec.Template.Spec.AutomountServiceAccountToken
	if tok == nil || *tok {
		t.Fatal("backup/restore/cleanup Jobs run restic only and must not get a ServiceAccount token")
	}
}
