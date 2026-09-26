package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func TestQuiesceWait(t *testing.T) {
	sent := time.Unix(1_800_000_000, 0)
	withRegex := &v1alpha1.EggBackup{SavedRegex: "Saved the game"} // timeout 60s
	noRegex := &v1alpha1.EggBackup{}                               // delay 10s

	tests := []struct {
		name       string
		h          *v1alpha1.EggBackup
		elapsed    time.Duration
		matched    bool
		wantReason string
		wantWait   time.Duration
	}{
		{"regex matched", withRegex, time.Second, true, v1alpha1.QuiesceReasonSaved, 0},
		{"regex pending polls every 3s", withRegex, time.Second, false, "", 3 * time.Second},
		{"regex pending near the deadline waits only the rest", withRegex, 59 * time.Second, false, "", time.Second},
		{"regex timed out", withRegex, 60 * time.Second, false, v1alpha1.QuiesceReasonTimeout, 0},
		{"no regex waits the delay", noRegex, 4 * time.Second, false, "", 6 * time.Second},
		{"no regex delay over", noRegex, 10 * time.Second, false, v1alpha1.QuiesceReasonDelay, 0},
		{"no regex ignores matched", noRegex, 0, true, "", 10 * time.Second},
	}
	for _, tc := range tests {
		reason, wait := quiesceWait(tc.h, sent, sent.Add(tc.elapsed), tc.matched)
		if reason != tc.wantReason || wait != tc.wantWait {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, reason, wait, tc.wantReason, tc.wantWait)
		}
	}
}

func TestResumeDecision(t *testing.T) {
	now := metav1.Now()
	sent := &v1alpha1.BackupQuiesceStatus{PodUID: "pod-a", BeforeSentAt: &now}
	done := &v1alpha1.BackupQuiesceStatus{PodUID: "pod-a", BeforeSentAt: &now, ResumedAt: &now}
	hooks := &v1alpha1.EggBackup{After: []string{"save-on"}}

	tests := []struct {
		name string
		q    *v1alpha1.BackupQuiesceStatus
		h    *v1alpha1.EggBackup
		uid  string
		want resumeAction
	}{
		{"nothing was paused", nil, hooks, "pod-a", resumeNone},
		{"already resumed", done, hooks, "pod-a", resumeNone},
		{"same pod gets after", sent, hooks, "pod-a", resumeSend},
		{"replaced pod is skipped", sent, hooks, "pod-b", resumeSkip},
		{"no pod is skipped", sent, hooks, "", resumeSkip},
		{"egg without after is skipped", sent, &v1alpha1.EggBackup{}, "pod-a", resumeSkip},
		{"egg gone is skipped", sent, nil, "pod-a", resumeSkip},
	}
	for _, tc := range tests {
		if got := resumeDecision(tc.q, tc.h, tc.uid); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStdinCommandPassesTheCommandAsAnArgument(t *testing.T) {
	got := stdinCommand(`say "hi"; rm -rf /`)
	if len(got) != 5 || got[4] != `say "hi"; rm -rf /` || got[2] != `printf '%s\n' "$1" > /proc/1/fd/0` {
		t.Fatalf("got %q", got)
	}
}
