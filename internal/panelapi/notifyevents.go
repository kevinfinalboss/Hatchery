package panelapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/mailer"
)

// Events an organization can be alerted about. The names are stable: they are stored in
// org_notification_muted and shown by the API and the UI.
const (
	eventGaveUp         = "gameserver.gave_up"
	eventBackupFailed   = "backup.failed"
	eventResumeFailed   = "backup.resume_failed"
	eventScheduleFailed = "schedule.failed"
	eventSuspended      = "gameserver.suspended"
)

var notificationEvents = []string{eventGaveUp, eventBackupFailed, eventResumeFailed, eventScheduleFailed, eventSuspended}

// eventMailKinds maps each event to its e-mail template.
var eventMailKinds = map[string]mailer.Kind{
	eventGaveUp:         mailer.KindAlertGaveUp,
	eventBackupFailed:   mailer.KindAlertBackupFailed,
	eventResumeFailed:   mailer.KindAlertResumeFailed,
	eventScheduleFailed: mailer.KindAlertScheduleFailed,
	eventSuspended:      mailer.KindAlertSuspended,
}

// occurrence is one thing that went wrong. Identity is stable for the same occurrence, which is
// what keeps it from being announced twice.
type occurrence struct {
	Event      string
	Identity   string
	GameServer string
	Detail     string
	At         time.Time
}

// detectOccurrences reads what the operator recorded in the status of one namespace's objects.
// It is pure: the age cut-off and deduplication happen in the notifier.
func detectOccurrences(gss []v1.GameServer, bkps []v1.GameServerBackup, schs []v1.GameServerSchedule) []occurrence {
	var out []occurrence
	for _, gs := range gss {
		c := apimeta.FindStatusCondition(gs.Status.Conditions, v1.ConditionCrashed)
		if c == nil || c.Status != metav1.ConditionTrue {
			continue
		}
		detail := c.Message
		if lc := gs.Status.LastCrash; lc != nil {
			parts := []string{}
			if lc.Reason != "" {
				parts = append(parts, lc.Reason)
			}
			parts = append(parts, fmt.Sprintf("exit %d", lc.ExitCode))
			if lc.OOMKilled {
				parts = append(parts, "OOM")
			}
			detail = strings.TrimSpace(c.Message + " — " + strings.Join(parts, " · "))
		}
		out = append(out, occurrence{
			Event: eventGaveUp, GameServer: gs.Name, Detail: detail, At: c.LastTransitionTime.Time,
			Identity: "gave_up/" + string(gs.UID) + "/" + strconv.FormatInt(c.LastTransitionTime.Unix(), 10),
		})
	}
	for _, b := range bkps {
		if b.Status.Phase == v1.GameServerBackupPhaseFailed {
			at := b.CreationTimestamp.Time
			if b.Status.CompletionTime != nil {
				at = b.Status.CompletionTime.Time
			}
			out = append(out, occurrence{
				Event: eventBackupFailed, GameServer: b.Spec.GameServerRef.Name, Detail: b.Name, At: at,
				Identity: "backup_failed/" + string(b.UID),
			})
		}
		if c := apimeta.FindStatusCondition(b.Status.Conditions, v1.BackupConditionResumed); c != nil &&
			c.Status == metav1.ConditionFalse && c.Reason == v1.QuiesceReasonSendFailed {
			out = append(out, occurrence{
				Event: eventResumeFailed, GameServer: b.Spec.GameServerRef.Name, Detail: strings.TrimSpace(b.Name + ": " + c.Message),
				At: c.LastTransitionTime.Time, Identity: "resume_failed/" + string(b.UID),
			})
		}
	}
	for _, s := range schs {
		run := s.Status.LastRun
		if run == nil || run.Result != v1.ScheduleRunFailed {
			continue
		}
		at := run.StartedAt.Time
		if run.FinishedAt != nil {
			at = run.FinishedAt.Time
		}
		out = append(out, occurrence{
			Event: eventScheduleFailed, GameServer: s.Spec.GameServerRef.Name, Detail: strings.TrimSpace(s.Name + ": " + run.Message),
			At: at, Identity: "schedule_failed/" + string(s.UID) + "/" + strconv.FormatInt(run.StartedAt.Unix(), 10),
		})
	}
	return out
}
