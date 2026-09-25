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
	"strings"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func TestValidateEmail(t *testing.T) {
	if got, err := validateEmail("  Kevin@Example.com "); err != nil || got != "kevin@example.com" {
		t.Fatalf("validateEmail = %q, %v", got, err)
	}
	for _, bad := range []string{"", "kevin", "Kevin <kevin@example.com>", "a@", "@b.com"} {
		if _, err := validateEmail(bad); err == nil {
			t.Errorf("validateEmail(%q) accepted", bad)
		}
	}
}

func TestValidatePasswordAndUsername(t *testing.T) {
	if validatePassword("1234567") == nil || validatePassword(strings.Repeat("a", 73)) == nil {
		t.Error("password length bounds not enforced")
	}
	if validatePassword("12345678") != nil || validatePassword(strings.Repeat("a", 72)) != nil {
		t.Error("valid password refused")
	}
	for _, ok := range []string{"kevin", "k.g", "Kevin_Gomes-1"} {
		if validateUsername(ok) != nil {
			t.Errorf("username %q refused", ok)
		}
	}
	for _, bad := range []string{"k", "kevin@x", "has space", strings.Repeat("a", 33)} {
		if validateUsername(bad) == nil {
			t.Errorf("username %q accepted", bad)
		}
	}
}

func TestValidateProfile(t *testing.T) {
	good := paneldb.Profile{DisplayName: "Kevin Gomes", Locale: "en", TimeZone: "America/Sao_Paulo",
		Discord: "kevin.g", MinecraftUsername: "Kevin_MC", SteamID: "76561197960287930"}
	if err := validateProfile(good); err != nil {
		t.Fatal(err)
	}
	if err := validateProfile(paneldb.Profile{}); err != nil {
		t.Fatalf("empty profile: %v", err)
	}
	bad := []paneldb.Profile{
		{DisplayName: strings.Repeat("a", 65)},
		{DisplayName: "line\nbreak"},
		{Locale: "fr"},
		{TimeZone: "Mars/Olympus"},
		{Discord: "Kevin#1234"},
		{MinecraftUsername: "ab"},
		{SteamID: "12345"},
	}
	for _, p := range bad {
		if validateProfile(p) == nil {
			t.Errorf("validateProfile(%+v) accepted", p)
		}
	}
}
