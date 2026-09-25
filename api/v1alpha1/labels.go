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

const (
	LabelManagedBy  = "app.kubernetes.io/managed-by"
	LabelGameServer = "gameservers.hatchery.io/gameserver"
	ManagedByValue  = "hatchery"
)

// GameServerLabels is the standard label set applied to every resource owned
// by the GameServer named name: its Pod, Service, PVC and sftp-agent Secret
// (all created by the GameServerController), and the Panel API's on-demand
// SFTP maintenance Pod. It's the Service's selector, which is exactly why the
// maintenance Pod needs to carry it too — that's what lets the same Service
// reach whichever Pod currently backs SFTP, sidecar or maintenance, without
// the client needing to know which one it is.
func GameServerLabels(name string) map[string]string {
	return map[string]string{
		LabelManagedBy:  ManagedByValue,
		LabelGameServer: name,
	}
}

// ModpackEggLabel marks an Egg whose servers are built from a modpack (the Panel shows the
// modpack picker and card for them). Value "true".
const ModpackEggLabel = "gameservers.hatchery.io/modpack-egg"

// IsModpack reports whether e is a modpack Egg (see ModpackEggLabel).
func (e *Egg) IsModpack() bool {
	return e.Labels[ModpackEggLabel] == "true"
}
