package panelapi

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

var detectAt = metav1.NewTime(time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC))

func cond(typ string, status metav1.ConditionStatus, reason, msg string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: status, Reason: reason, Message: msg, LastTransitionTime: detectAt}
}

func TestDetectOccurrences(t *testing.T) {
	gaveUp := v1.GameServer{ObjectMeta: metav1.ObjectMeta{Name: "mc", UID: types.UID("gs-1")}}
	gaveUp.Status.Conditions = []metav1.Condition{cond(v1.ConditionCrashed, metav1.ConditionTrue, "CrashLoop", "stopped after 3 crashes")}
	gaveUp.Status.LastCrash = &v1.GameServerCrash{At: detectAt, ExitCode: 137, Reason: "OOMKilled", OOMKilled: true}
	fine := v1.GameServer{ObjectMeta: metav1.ObjectMeta{Name: "ok", UID: types.UID("gs-2")}}
	fine.Status.Conditions = []metav1.Condition{cond(v1.ConditionCrashed, metav1.ConditionFalse, "NotCrashed", "")}

	failed := v1.GameServerBackup{ObjectMeta: metav1.ObjectMeta{Name: "mc-abc", UID: types.UID("b-1")}}
	failed.Spec.GameServerRef.Name = "mc"
	failed.Status.Phase = v1.GameServerBackupPhaseFailed
	failed.Status.CompletionTime = &detectAt
	done := v1.GameServerBackup{ObjectMeta: metav1.ObjectMeta{Name: "mc-ok", UID: types.UID("b-2")}}
	done.Status.Phase = v1.GameServerBackupPhaseCompleted
	notResumed := v1.GameServerBackup{ObjectMeta: metav1.ObjectMeta{Name: "mc-r", UID: types.UID("b-3")}}
	notResumed.Spec.GameServerRef.Name = "mc"
	notResumed.Status.Phase = v1.GameServerBackupPhaseCompleted
	notResumed.Status.Conditions = []metav1.Condition{cond(v1.BackupConditionResumed, metav1.ConditionFalse, "SendFailed", "save-on failed 5 times")}
	replaced := v1.GameServerBackup{ObjectMeta: metav1.ObjectMeta{Name: "mc-p", UID: types.UID("b-4")}}
	replaced.Status.Phase = v1.GameServerBackupPhaseCompleted
	replaced.Status.Conditions = []metav1.Condition{cond(v1.BackupConditionResumed, metav1.ConditionFalse, "PodReplaced", "")}

	sch := v1.GameServerSchedule{ObjectMeta: metav1.ObjectMeta{Name: "nightly", UID: types.UID("s-1")}}
	sch.Spec.GameServerRef.Name = "mc"
	sch.Status.LastRun = &v1.ScheduleRun{StartedAt: detectAt, FinishedAt: &detectAt, Result: v1.ScheduleRunFailed, Message: "task 1 (Backup): no backup target"}
	okSch := v1.GameServerSchedule{ObjectMeta: metav1.ObjectMeta{Name: "fine", UID: types.UID("s-2")}}
	okSch.Status.LastRun = &v1.ScheduleRun{StartedAt: detectAt, Result: v1.ScheduleRunSucceeded}

	got := detectOccurrences([]v1.GameServer{gaveUp, fine}, []v1.GameServerBackup{failed, done, notResumed, replaced}, []v1.GameServerSchedule{sch, okSch})
	byEvent := map[string]occurrence{}
	for _, o := range got {
		byEvent[o.Event] = o
	}
	if len(got) != 4 || len(byEvent) != 4 {
		t.Fatalf("occurrences = %+v", got)
	}
	g := byEvent[eventGaveUp]
	if g.GameServer != "mc" || !g.At.Equal(detectAt.Time) || !strings.Contains(g.Detail, "137") || !strings.Contains(g.Detail, "OOM") || g.Identity != "gave_up/gs-1/"+itoa(detectAt.Unix()) {
		t.Errorf("gave_up = %+v", g)
	}
	if b := byEvent[eventBackupFailed]; b.GameServer != "mc" || b.Identity != "backup_failed/b-1" || !strings.Contains(b.Detail, "mc-abc") {
		t.Errorf("backup.failed = %+v", b)
	}
	if r := byEvent[eventResumeFailed]; r.Identity != "resume_failed/b-3" || !strings.Contains(r.Detail, "save-on failed") {
		t.Errorf("resume_failed = %+v", r)
	}
	if s := byEvent[eventScheduleFailed]; s.GameServer != "mc" || s.Identity != "schedule_failed/s-1/"+itoa(detectAt.Unix()) || !strings.Contains(s.Detail, "nightly") || !strings.Contains(s.Detail, "no backup target") {
		t.Errorf("schedule.failed = %+v", s)
	}
	// Stable across calls: the identity is what deduplicates.
	again := detectOccurrences([]v1.GameServer{gaveUp}, nil, nil)
	if again[0].Identity != g.Identity {
		t.Error("identity changed between calls")
	}
}
