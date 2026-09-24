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

package panelapi

import (
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestEffectivePermissions(t *testing.T) {
	member := &orgAccess{Role: paneldb.RoleMember, Grants: []paneldb.Grant{
		{GameServer: "*", Permissions: []paneldb.Permission{paneldb.PermConsoleRead}},
		{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}},
	}}
	if !member.Can("mc", paneldb.PermPower) || !member.Can("mc", paneldb.PermConsoleRead) {
		t.Fatal("mc must combine its own grant with *")
	}
	if !member.Can("brand-new", paneldb.PermConsoleRead) || member.Can("brand-new", paneldb.PermPower) {
		t.Fatal("* must apply to any server, and only with its own permissions")
	}
	onlyMC := &orgAccess{Role: paneldb.RoleMember, Grants: []paneldb.Grant{{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}}}}
	if onlyMC.Sees("other") || !onlyMC.Sees("mc") {
		t.Fatal("Sees must be true only where some grant exists")
	}
	admin := &orgAccess{Role: paneldb.RoleAdmin}
	if !admin.Can("anything", paneldb.PermSchedules) || len(admin.Effective("x")) != len(paneldb.AllPermissions) {
		t.Fatal("admin has every permission everywhere")
	}
	none := &orgAccess{Role: paneldb.RoleMember}
	if none.Sees("mc") || none.Effective("mc") != nil {
		t.Fatal("a member without grants sees nothing")
	}
}
