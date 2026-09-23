package v1alpha1

import (
	"strings"
	"testing"
	"time"
)

func sched(cron, tz string, tasks ...ScheduleTask) GameServerScheduleSpec {
	return GameServerScheduleSpec{GameServerRef: GameServerRef{Name: "mc"}, Cron: cron, TimeZone: tz, OnlyWhenRunning: true, Tasks: tasks}
}

func TestValidateSchedule(t *testing.T) {
	restart := ScheduleTask{Action: ScheduleActionRestart}
	backup := ScheduleTask{Action: ScheduleActionBackup, KeepLast: 7}
	ok := []GameServerScheduleSpec{
		sched("0 4 * * *", "America/Sao_Paulo", ScheduleTask{Action: ScheduleActionCommand, Command: "say hi"}, ScheduleTask{Action: ScheduleActionBackup, DelaySeconds: 300}, restart),
		sched("*/5 * * * *", "", restart),
		sched("0 * * * *", "UTC", backup),
	}
	for _, s := range ok {
		if msgs := ValidateSchedule(s); len(msgs) != 0 {
			t.Errorf("%q: unexpected %v", s.Cron, msgs)
		}
	}
	bad := map[string]GameServerScheduleSpec{
		"bad cron":                 sched("61 * * * *", "", restart),
		"six fields":               sched("0 0 4 * * *", "", restart),
		"unknown zone":             sched("0 4 * * *", "Mars/Olympus", restart),
		"no tasks":                 sched("0 4 * * *", ""),
		"command without text":     sched("0 4 * * *", "", ScheduleTask{Action: ScheduleActionCommand}),
		"multi-line command":       sched("0 4 * * *", "", ScheduleTask{Action: ScheduleActionCommand, Command: "a\nb"}),
		"command on restart":       sched("0 4 * * *", "", ScheduleTask{Action: ScheduleActionRestart, Command: "x"}),
		"keepLast on restart":      sched("0 4 * * *", "", ScheduleTask{Action: ScheduleActionRestart, KeepLast: 2}),
		"every minute":             sched("* * * * *", "", restart),
		"backup every 30 min":      sched("*/30 * * * *", "", backup),
		"delays longer than cycle": sched("*/5 * * * *", "", ScheduleTask{Action: ScheduleActionRestart, DelaySeconds: 400}),
	}
	for name, s := range bad {
		if msgs := ValidateSchedule(s); len(msgs) == 0 {
			t.Errorf("%s: want a validation error", name)
		}
	}
}

func TestNextRunHonoursTimeZoneAndDST(t *testing.T) {
	s := sched("0 4 * * *", "America/New_York", ScheduleTask{Action: ScheduleActionRestart})
	ny, _ := time.LoadLocation("America/New_York")
	// 2026-03-08 is the US spring-forward day: 04:00 local still exists and must be kept.
	after := time.Date(2026, 3, 7, 12, 0, 0, 0, ny)
	next, err := NextRun(s, after)
	if err != nil {
		t.Fatal(err)
	}
	if got := next.In(ny); got.Hour() != 4 || got.Day() != 8 {
		t.Fatalf("next = %v, want 2026-03-08 04:00 New York time", got)
	}
	next2, _ := NextRun(s, next)
	if got := next2.In(ny); got.Hour() != 4 || got.Day() != 9 {
		t.Fatalf("second next = %v, want 04:00 on the 9th", got)
	}
	if !strings.HasSuffix(next.Location().String(), "UTC") {
		t.Fatalf("NextRun must return UTC for status fields, got %v", next.Location())
	}
}
