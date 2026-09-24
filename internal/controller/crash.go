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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CrashPolicy bounds automatic restarts: a server whose game process crashes Limit times within Window
// is left stopped instead of restarted again.
type CrashPolicy struct {
	Limit  int
	Window time.Duration
}

// DefaultCrashPolicy is used when the reconciler is given none: 3 crashes in 10 minutes.
var DefaultCrashPolicy = CrashPolicy{Limit: 3, Window: 10 * time.Minute}

func (p CrashPolicy) orDefault() CrashPolicy {
	if p.Limit < 1 || p.Window <= 0 {
		return DefaultCrashPolicy
	}
	return p
}

// crashRestartDelays is how long a crashed server waits before it is restarted, by how many crashes
// the window holds (1st, 2nd, ...). Past the end the last value repeats.
var crashRestartDelays = []time.Duration{10 * time.Second, 30 * time.Second}

func restartDelay(crashes int) time.Duration {
	if crashes < 1 {
		crashes = 1
	}
	if crashes > len(crashRestartDelays) {
		crashes = len(crashRestartDelays)
	}
	return crashRestartDelays[crashes-1]
}

const (
	// crashLogLines is how much of the crashed console is kept.
	crashLogLines int64 = 200
	// crashLogMaxBytes caps the kept console so the ConfigMap stays small.
	crashLogMaxBytes = 64 << 10
)

// trimCrashLog keeps the last crashLogMaxBytes of the console, starting on a whole line.
func trimCrashLog(s string) string {
	if len(s) <= crashLogMaxBytes {
		return s
	}
	s = s[len(s)-crashLogMaxBytes:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

type crashAction int

const (
	// crashCleanExit: the game exited with code 0 on its own ("stop" typed in the console). Not a crash.
	crashCleanExit crashAction = iota + 1
	// crashRestart: restart the server once restartAt has passed.
	crashRestart
	// crashGiveUp: leave the server stopped (reason CrashLoop or AutoRestartDisabled).
	crashGiveUp
)

type crashDecision struct {
	action    crashAction
	reason    string        // crashGiveUp only
	recent    []metav1.Time // the crash window after this crash, to store in status.recentCrashes
	restartAt time.Time     // crashRestart only
}

func isOOMKilled(t *corev1.ContainerStateTerminated) bool { return t.Reason == "OOMKilled" }

// decideCrash picks what to do about a game container that ended while the server was meant to run.
// The crash is identified by its finishedAt, so deciding twice about the same container (a repeated
// reconcile, or an operator restarted mid-wait) neither counts it twice nor moves restartAt.
func decideCrash(term *corev1.ContainerStateTerminated, autoRestart bool, recent []metav1.Time, p CrashPolicy) crashDecision {
	if term.ExitCode == 0 && !isOOMKilled(term) {
		return crashDecision{action: crashCleanExit, recent: recent}
	}
	p = p.orDefault()
	at := term.FinishedAt.Time

	var kept []metav1.Time
	seen := false
	for _, t := range recent {
		if at.Sub(t.Time) >= p.Window {
			continue
		}
		if t.Unix() == at.Unix() {
			seen = true
		}
		kept = append(kept, t)
	}
	if !seen {
		kept = append(kept, metav1.NewTime(at))
	}

	d := crashDecision{recent: kept}
	switch {
	case !autoRestart:
		d.action, d.reason = crashGiveUp, "AutoRestartDisabled"
	case len(kept) >= p.Limit:
		d.action, d.reason = crashGiveUp, "CrashLoop"
	default:
		d.action = crashRestart
		d.restartAt = at.Add(restartDelay(len(kept)))
	}
	return d
}

// crashMessage is the Crashed condition's message when the controller gives up.
func crashMessage(d crashDecision, term *corev1.ContainerStateTerminated, p CrashPolicy) string {
	if d.reason == "AutoRestartDisabled" {
		oom := ""
		if isOOMKilled(term) {
			oom = " (out of memory)"
		}
		return fmt.Sprintf("crashed with exit code %d%s; automatic restart is off", term.ExitCode, oom)
	}
	return fmt.Sprintf("stopped after %d crashes in %s; last exit code %d", len(d.recent), p.orDefault().Window, term.ExitCode)
}
