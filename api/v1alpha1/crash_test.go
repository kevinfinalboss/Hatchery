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
	"testing"

	"k8s.io/utils/ptr"
)

func TestAutoRestartDefaultsToOn(t *testing.T) {
	gs := &GameServer{}
	if !gs.AutoRestartEnabled() {
		t.Fatal("an unset autoRestart must mean on")
	}
	gs.Spec.AutoRestart = ptr.To(false)
	if gs.AutoRestartEnabled() {
		t.Fatal("autoRestart: false must turn it off")
	}
	gs.Spec.AutoRestart = ptr.To(true)
	if !gs.AutoRestartEnabled() {
		t.Fatal("autoRestart: true must keep it on")
	}
}

func TestCrashLogConfigMapName(t *testing.T) {
	if got := CrashLogConfigMapName("mc"); got != "mc-crash-log" {
		t.Fatalf("got %q", got)
	}
}
