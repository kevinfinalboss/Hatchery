/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/robfig/cron/v3"
)

const (
	// ScheduleLabel marks a GameServerBackup made by a schedule (value: the schedule's name).
	ScheduleLabel = "gameservers.hatchery.io/schedule"
	// ScheduleRunAnnotation carries the ActiveScheduleRun.ID that made a backup.
	ScheduleRunAnnotation = "gameservers.hatchery.io/schedule-run"
	// RunNowAnnotation on a GameServerSchedule triggers one run per distinct value.
	RunNowAnnotation = "gameservers.hatchery.io/run-now"

	minScheduleInterval       = 5 * time.Minute
	minBackupScheduleInterval = time.Hour
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func scheduleLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(tz)
}

// NextRun is the first fire time strictly after `after`, in UTC.
func NextRun(spec GameServerScheduleSpec, after time.Time) (time.Time, error) {
	loc, err := scheduleLocation(spec.TimeZone)
	if err != nil {
		return time.Time{}, err
	}
	s, err := cronParser.Parse(spec.Cron)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(after.In(loc)).UTC(), nil
}

// ValidateSchedule checks what the CRD schema cannot: cron and time zone syntax, per-action
// fields, the minimum interval and that the delays fit in it. Used by the webhook and the Panel.
func ValidateSchedule(spec GameServerScheduleSpec) []string {
	var msgs []string
	loc, err := scheduleLocation(spec.TimeZone)
	if err != nil {
		msgs = append(msgs, fmt.Sprintf("timeZone %q is not a known IANA time zone", spec.TimeZone))
		loc = time.UTC
	}
	parsed, err := cronParser.Parse(spec.Cron)
	if err != nil {
		msgs = append(msgs, fmt.Sprintf("cron %q: %v", spec.Cron, err))
	}
	if len(spec.Tasks) == 0 {
		msgs = append(msgs, "at least one task is required")
	}
	if len(spec.Tasks) > 10 {
		msgs = append(msgs, "at most 10 tasks")
	}
	hasBackup := false
	var totalDelay time.Duration
	for i, t := range spec.Tasks {
		totalDelay += time.Duration(t.DelaySeconds) * time.Second
		if t.DelaySeconds < 0 || t.DelaySeconds > 900 {
			msgs = append(msgs, fmt.Sprintf("tasks[%d].delaySeconds must be between 0 and 900", i))
		}
		switch t.Action {
		case ScheduleActionCommand:
			if strings.TrimSpace(t.Command) == "" {
				msgs = append(msgs, fmt.Sprintf("tasks[%d].command is required for Command", i))
			}
			if len(t.Command) > 512 || strings.IndexFunc(t.Command, unicode.IsControl) >= 0 {
				msgs = append(msgs, fmt.Sprintf("tasks[%d].command must be one line of at most 512 characters", i))
			}
		case ScheduleActionRestart, ScheduleActionStart, ScheduleActionStop, ScheduleActionBackup:
			if t.Command != "" {
				msgs = append(msgs, fmt.Sprintf("tasks[%d].command is only for Command", i))
			}
		default:
			msgs = append(msgs, fmt.Sprintf("tasks[%d].action %q is not one of Command, Restart, Start, Stop, Backup", i, t.Action))
		}
		if t.Action == ScheduleActionBackup {
			hasBackup = true
		} else if t.KeepLast != 0 {
			msgs = append(msgs, fmt.Sprintf("tasks[%d].keepLast is only for Backup", i))
		}
	}
	if parsed != nil {
		// The shortest gap among the next fire times, from a fixed reference so the answer does
		// not depend on today's date.
		ref := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
		prev := parsed.Next(ref)
		minGap := time.Duration(1<<62 - 1)
		for i := 0; i < 50; i++ {
			next := parsed.Next(prev)
			if gap := next.Sub(prev); gap < minGap {
				minGap = gap
			}
			prev = next
		}
		limit := minScheduleInterval
		if hasBackup {
			limit = minBackupScheduleInterval
		}
		if minGap < limit {
			msgs = append(msgs, fmt.Sprintf("runs may be at most every %s (this cron fires every %s)", limit, minGap))
		}
		if totalDelay >= minGap {
			msgs = append(msgs, fmt.Sprintf("the task delays add up to %s, which does not fit between runs (%s)", totalDelay, minGap))
		}
	}
	return msgs
}
