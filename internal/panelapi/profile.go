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
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

var (
	usernameRE  = regexp.MustCompile(`^[A-Za-z0-9._-]{2,32}$`)
	discordRE   = regexp.MustCompile(`^[a-z0-9_.]{2,32}$`)
	minecraftRE = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	steamIDRE   = regexp.MustCompile(`^7656119\d{10}$`)
)

// validateEmail accepts a bare address (no display name) and returns its
// stored form (lowercase).
func validateEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return "", errors.New("invalid e-mail address")
	}
	return paneldb.NormalizeEmail(s), nil
}

// validatePassword: 8 to 72 bytes (bcrypt silently ignores anything past 72).
func validatePassword(p string) error {
	if len(p) < 8 || len(p) > 72 {
		return errors.New("password must be 8 to 72 bytes long")
	}
	return nil
}

// validateUsername: new usernames never contain "@", which is how login tells
// a username from an e-mail.
func validateUsername(u string) error {
	if !usernameRE.MatchString(u) {
		return errors.New("username must be 2 to 32 characters: letters, digits, '.', '_' or '-'")
	}
	return nil
}

// validateDisplayName (defined in gameservers.go, shared with the GameServer
// displayName field) already enforces the same rule this needs: at most 64
// characters, no control characters.

func validateProfile(p paneldb.Profile) error {
	if err := validateDisplayName(p.DisplayName); err != nil {
		return err
	}
	if p.Locale != "" && p.Locale != "pt-BR" && p.Locale != "en" {
		return errors.New(`locale must be "", "pt-BR" or "en"`)
	}
	if p.TimeZone != "" {
		if _, err := time.LoadLocation(p.TimeZone); err != nil || p.TimeZone == "Local" {
			return fmt.Errorf("unknown time zone %q", p.TimeZone)
		}
	}
	if p.Discord != "" && !discordRE.MatchString(p.Discord) {
		return errors.New("discord must be a Discord username: 2 to 32 lowercase letters, digits, '_' or '.'")
	}
	if p.MinecraftUsername != "" && !minecraftRE.MatchString(p.MinecraftUsername) {
		return errors.New("minecraft username must be 3 to 16 letters, digits or '_'")
	}
	if p.SteamID != "" && !steamIDRE.MatchString(p.SteamID) {
		return errors.New("steam id must be a 17-digit SteamID64")
	}
	return nil
}
