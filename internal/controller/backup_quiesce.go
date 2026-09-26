package controller

import (
	"time"

	v1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

const (
	// quiescePollInterval is how often the console is re-read while waiting for Egg.spec.backup.savedRegex.
	quiescePollInterval = 3 * time.Second
	// resumeMaxAttempts is how many times After is tried before giving up (the world stays unsaved
	// until the server restarts, so the backup's Resumed condition tells the user).
	resumeMaxAttempts = 5
	// resumeRetryStep is the wait after the n-th failed attempt, times n.
	resumeRetryStep = 10 * time.Second
)

// quiesceWait decides whether the wait after the Before commands is over. matched reports whether
// SavedRegex matched the console written since sentAt; it is ignored when the Egg has no regex.
// An empty reason means keep waiting for wait.
func quiesceWait(h *v1alpha1.EggBackup, sentAt, now time.Time, matched bool) (reason string, wait time.Duration) {
	if h.SavedRegex != "" {
		if matched {
			return v1alpha1.QuiesceReasonSaved, 0
		}
		deadline := sentAt.Add(h.Timeout())
		if !now.Before(deadline) {
			return v1alpha1.QuiesceReasonTimeout, 0
		}
		return "", min(quiescePollInterval, deadline.Sub(now))
	}
	deadline := sentAt.Add(h.Delay())
	if !now.Before(deadline) {
		return v1alpha1.QuiesceReasonDelay, 0
	}
	return "", deadline.Sub(now)
}

type resumeAction int

const (
	resumeNone resumeAction = iota // nothing was paused, or After is already dealt with
	resumeSkip                     // record it as done without sending (other Pod, nothing to send)
	resumeSend                     // send After to the Pod that got Before
)

// resumeDecision says what to do about the After commands. A Pod other than the one that got
// Before (or none) starts with saving on, so it gets nothing.
func resumeDecision(q *v1alpha1.BackupQuiesceStatus, h *v1alpha1.EggBackup, currentPodUID string) resumeAction {
	if q == nil || q.BeforeSentAt == nil || q.ResumedAt != nil {
		return resumeNone
	}
	if h == nil || len(h.After) == 0 || currentPodUID == "" || currentPodUID != q.PodUID {
		return resumeSkip
	}
	return resumeSend
}
