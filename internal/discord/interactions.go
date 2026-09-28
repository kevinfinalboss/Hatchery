// Package discord holds what the Panel needs to speak to Discord: interaction signatures and
// types, a small REST client, the slash command definitions and a presence-only Gateway client.
package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ParsePublicKey decodes the application's hex public key (Developer Portal → General Information).
func ParsePublicKey(hexKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("discord public key must be 64 hex characters")
	}
	return ed25519.PublicKey(raw), nil
}

// VerifySignature checks an interaction request: Discord signs timestamp+body with the
// application's key (X-Signature-Ed25519, X-Signature-Timestamp).
func VerifySignature(key ed25519.PublicKey, signatureHex, timestamp string, body []byte) bool {
	sig, err := hex.DecodeString(signatureHex)
	if err != nil || len(sig) != ed25519.SignatureSize || len(key) != ed25519.PublicKeySize {
		return false
	}
	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(append(msg, timestamp...), body...)
	return ed25519.Verify(key, msg, sig)
}

// Interaction types.
const (
	InteractionPing         = 1
	InteractionCommand      = 2
	InteractionAutocomplete = 4
)

// Response types.
const (
	ResponsePong               = 1
	ResponseMessage            = 4
	ResponseDeferredMessage    = 5
	ResponseAutocompleteResult = 8
	FlagEphemeral              = 64
	OptionString               = 3
	contextGuild               = 0
	integrationGuildInstall    = 0
	maxAutocompleteChoices     = 25
	MaxAutocompleteChoices     = maxAutocompleteChoices
	maxMessageLength           = 2000
	MaxMessageLength           = maxMessageLength
	defaultAPIBase             = "https://discord.com/api/v10"
	defaultAuthorizeURL        = "https://discord.com/oauth2/authorize"
	PermissionsNone            = "0"
	ScopeIdentify              = "identify"
	ScopeBot                   = "bot"
	ScopeApplicationsCommands  = "applications.commands"
	InteractionsEndpointPath   = "/api/v1/discord/interactions"
	OAuthCallbackPath          = "/api/v1/discord/oauth/callback"
)

// User is a Discord user.
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// Interaction is the part of an incoming interaction the Panel reads.
type Interaction struct {
	ID      string `json:"id"`
	Type    int    `json:"type"`
	Token   string `json:"token"`
	GuildID string `json:"guild_id"`
	// Locale is the language of the user's Discord client (e.g. "pt-BR", "en-US").
	Locale string `json:"locale"`
	Member *struct {
		User User `json:"user"`
	} `json:"member"`
	User *User           `json:"user"`
	Data InteractionData `json:"data"`
}

// UserID is who ran the interaction (member.user in a guild, user in a DM).
func (i *Interaction) UserID() string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

// InteractionData is the command and its options.
type InteractionData struct {
	Name    string   `json:"name"`
	Options []Option `json:"options"`
}

// Option is one command option as sent by Discord.
type Option struct {
	Name    string          `json:"name"`
	Type    int             `json:"type"`
	Value   json.RawMessage `json:"value"`
	Focused bool            `json:"focused"`
}

// String returns a string option ("" when absent).
func (d InteractionData) String(name string) string {
	for _, o := range d.Options {
		if o.Name == name {
			var s string
			if json.Unmarshal(o.Value, &s) == nil {
				return s
			}
		}
	}
	return ""
}

// Focused is the option being autocompleted.
func (d InteractionData) Focused() string {
	for _, o := range d.Options {
		if o.Focused {
			return o.Name
		}
	}
	return ""
}

// Choice is an autocomplete suggestion.
type Choice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Response is what the Panel answers an interaction with.
type Response struct {
	Type int           `json:"type"`
	Data *ResponseData `json:"data,omitempty"`
}

// ResponseData is a message or a list of autocomplete choices.
type ResponseData struct {
	Content string   `json:"content,omitempty"`
	Flags   int      `json:"flags,omitempty"`
	Choices []Choice `json:"choices,omitempty"`
}

// Ephemeral is a message only the user who ran the command sees.
func Ephemeral(content string) Response {
	return Response{Type: ResponseMessage, Data: &ResponseData{Content: Truncate(content), Flags: FlagEphemeral}}
}

// Deferred acknowledges a slow command; the message is completed later with EditOriginal.
func Deferred() Response {
	return Response{Type: ResponseDeferredMessage, Data: &ResponseData{Flags: FlagEphemeral}}
}

// Choices answers an autocomplete (at most 25, Discord's limit).
func Choices(c []Choice) Response {
	if len(c) > maxAutocompleteChoices {
		c = c[:maxAutocompleteChoices]
	}
	if c == nil {
		c = []Choice{}
	}
	return Response{Type: ResponseAutocompleteResult, Data: &ResponseData{Choices: c}}
}

// Truncate keeps a message within Discord's 2000 characters.
func Truncate(s string) string {
	r := []rune(s)
	if len(r) <= maxMessageLength {
		return s
	}
	return string(r[:maxMessageLength-1]) + "…"
}
