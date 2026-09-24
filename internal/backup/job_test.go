package backup

import (
	"strings"
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

// restic refuses `forget --tag X` without a --keep-* policy ("no policy was specified"), so the cleanup
// Job must forget the tagged snapshots by ID — otherwise every deleted backup leaves its snapshot behind.
func TestCleanupJobForgetsSnapshotsByID(t *testing.T) {
	bkp := &gameserversv1alpha1.GameServerBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "hatchery-acme"},
		Spec: gameserversv1alpha1.GameServerBackupSpec{Destination: gameserversv1alpha1.BackupDestination{
			S3: &gameserversv1alpha1.S3Destination{Bucket: "bkt"},
		}},
	}
	c := CleanupJob(bkp).Spec.Template.Spec.Containers[0]
	script := strings.Join(append(append([]string{}, c.Command...), c.Args...), " ")
	if !strings.Contains(script, `restic forget --prune $ids`) {
		t.Fatalf("cleanup script must forget snapshots by ID, got:\n%s", script)
	}
	if strings.Contains(script, `forget --tag`) {
		t.Fatalf("cleanup script must not rely on a policy-less `forget --tag`, got:\n%s", script)
	}
}
