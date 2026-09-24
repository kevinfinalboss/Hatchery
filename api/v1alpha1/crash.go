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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// GameServerCrash describes how a server's game process ended when it crashed.
type GameServerCrash struct {
	// At is when the game container finished (its terminated.finishedAt).
	At metav1.Time `json:"at"`

	// ExitCode is the game process' exit code.
	ExitCode int32 `json:"exitCode"`

	// Reason is the kubelet's reason for the termination (Error, OOMKilled, ...).
	// +optional
	Reason string `json:"reason,omitempty"`

	// OOMKilled is true when the container was killed for going over its memory limit.
	// +optional
	OOMKilled bool `json:"oomKilled,omitempty"`
}

// CrashLogConfigMapName names the ConfigMap holding the console tail of a server's last crash. The
// operator writes it; the Panel API reads it.
func CrashLogConfigMapName(gameServer string) string { return gameServer + "-crash-log" }

// Keys of the crash log ConfigMap.
const (
	CrashLogKeyLog       = "log"
	CrashLogKeyAt        = "at"
	CrashLogKeyExitCode  = "exitCode"
	CrashLogKeyReason    = "reason"
	CrashLogKeyOOMKilled = "oomKilled"
)

// AutoRestartEnabled reports whether a crash restarts the server. Unset means on.
func (gs *GameServer) AutoRestartEnabled() bool {
	return gs.Spec.AutoRestart == nil || *gs.Spec.AutoRestart
}
