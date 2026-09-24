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

package controller

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func terminated(exit int32, reason string, at time.Time) *corev1.ContainerStateTerminated {
	return &corev1.ContainerStateTerminated{ExitCode: exit, Reason: reason, FinishedAt: metav1.NewTime(at)}
}

func TestDecideCrash(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	ago := func(d time.Duration) metav1.Time { return metav1.NewTime(base.Add(-d)) }
	p := CrashPolicy{Limit: 3, Window: 10 * time.Minute}

	tests := []struct {
		name        string
		term        *corev1.ContainerStateTerminated
		autoRestart bool
		recent      []metav1.Time
		wantAction  crashAction
		wantReason  string
		wantRecent  int
		wantDelay   time.Duration
	}{
		{name: "exit 0 is a stop", term: terminated(0, "Completed", base), autoRestart: true, wantAction: crashCleanExit},
		{name: "OOM with exit 0 is still a crash", term: terminated(0, "OOMKilled", base), autoRestart: true,
			wantAction: crashRestart, wantRecent: 1, wantDelay: 10 * time.Second},
		{name: "first crash restarts after 10s", term: terminated(1, "Error", base), autoRestart: true,
			wantAction: crashRestart, wantRecent: 1, wantDelay: 10 * time.Second},
		{name: "second crash restarts after 30s", term: terminated(1, "Error", base), autoRestart: true,
			recent: []metav1.Time{ago(time.Minute)}, wantAction: crashRestart, wantRecent: 2, wantDelay: 30 * time.Second},
		{name: "third crash in the window gives up", term: terminated(137, "OOMKilled", base), autoRestart: true,
			recent: []metav1.Time{ago(5 * time.Minute), ago(time.Minute)}, wantAction: crashGiveUp, wantReason: "CrashLoop", wantRecent: 3},
		{name: "crashes older than the window do not count", term: terminated(1, "Error", base), autoRestart: true,
			recent: []metav1.Time{ago(11 * time.Minute), ago(20 * time.Minute)}, wantAction: crashRestart, wantRecent: 1, wantDelay: 10 * time.Second},
		{name: "the same crash seen again is not counted twice", term: terminated(1, "Error", base), autoRestart: true,
			recent: []metav1.Time{ago(time.Minute), ago(0)}, wantAction: crashRestart, wantRecent: 2, wantDelay: 30 * time.Second},
		{name: "autoRestart off gives up on the first crash", term: terminated(1, "Error", base), autoRestart: false,
			wantAction: crashGiveUp, wantReason: "AutoRestartDisabled", wantRecent: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := decideCrash(tc.term, tc.autoRestart, tc.recent, p)
			if d.action != tc.wantAction || d.reason != tc.wantReason {
				t.Fatalf("got action %v reason %q, want %v %q", d.action, d.reason, tc.wantAction, tc.wantReason)
			}
			if tc.wantAction == crashCleanExit {
				return
			}
			if len(d.recent) != tc.wantRecent {
				t.Fatalf("got %d recent crashes, want %d: %v", len(d.recent), tc.wantRecent, d.recent)
			}
			if tc.wantAction == crashRestart && !d.restartAt.Equal(base.Add(tc.wantDelay)) {
				t.Fatalf("restartAt = %v, want %v after the crash", d.restartAt.Sub(base), tc.wantDelay)
			}
		})
	}
}

func TestDecideCrashFallsBackToTheDefaultPolicy(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	recent := []metav1.Time{metav1.NewTime(base.Add(-time.Minute)), metav1.NewTime(base.Add(-2 * time.Minute))}
	if d := decideCrash(terminated(1, "Error", base), true, recent, CrashPolicy{}); d.action != crashGiveUp {
		t.Fatalf("a zero policy must behave as 3 in 10m, got %v", d.action)
	}
}

func TestRestartDelay(t *testing.T) {
	for crashes, want := range map[int]time.Duration{0: 10 * time.Second, 1: 10 * time.Second, 2: 30 * time.Second, 5: 30 * time.Second} {
		if got := restartDelay(crashes); got != want {
			t.Errorf("restartDelay(%d) = %v, want %v", crashes, got, want)
		}
	}
}

func TestTrimCrashLog(t *testing.T) {
	if got := trimCrashLog("short\nlog\n"); got != "short\nlog\n" {
		t.Fatalf("a short log must be kept as is, got %q", got)
	}
	long := strings.Repeat("0123456789abcdef\n", 5000) // 85000 bytes
	got := trimCrashLog(long)
	if len(got) > crashLogMaxBytes {
		t.Fatalf("got %d bytes, limit is %d", len(got), crashLogMaxBytes)
	}
	if !strings.HasPrefix(got, "0123456789abcdef\n") || !strings.HasSuffix(long, got) {
		t.Fatal("the cut must keep the end of the log and start on a whole line")
	}
}

func TestCrashMessage(t *testing.T) {
	p := CrashPolicy{Limit: 3, Window: 10 * time.Minute}
	loop := crashDecision{action: crashGiveUp, reason: "CrashLoop", recent: make([]metav1.Time, 3)}
	if got := crashMessage(loop, terminated(1, "Error", time.Now()), p); got != "stopped after 3 crashes in 10m0s; last exit code 1" {
		t.Fatalf("got %q", got)
	}
	off := crashDecision{action: crashGiveUp, reason: "AutoRestartDisabled"}
	if got := crashMessage(off, terminated(137, "OOMKilled", time.Now()), p); got != "crashed with exit code 137 (out of memory); automatic restart is off" {
		t.Fatalf("got %q", got)
	}
}
