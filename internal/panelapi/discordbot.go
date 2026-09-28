package panelapi

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kevinfinalboss/Hatchery/internal/discord"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// DiscordBot is the Discord application the Panel runs its bot as. nil on Server = feature off.
type DiscordBot struct {
	Client    *discord.Client
	PublicKey ed25519.PublicKey
}

var discordLog = logf.Log.WithName("panelapi-discord")

const (
	settingPlatformGuild     = "discord_platform_guild"
	settingPlatformGuildName = "discord_platform_guild_name"
	discordStateTTL          = 10 * time.Minute

	flowLink     = "discord:link"
	flowOrg      = "discord:org"
	flowPlatform = "discord:platform"
)

func (s *Server) discordEnabled() bool {
	return s.Bot != nil && s.Bot.Client != nil && s.PublicURL != ""
}

// discordOn wraps the Discord routes: they do not exist when the bot is not configured.
func (s *Server) discordOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.discordEnabled() {
			writeError(w, http.StatusNotFound, "Discord is not configured on this platform")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) discordRedirectURI() string {
	return strings.TrimRight(s.PublicURL, "/") + discord.OAuthCallbackPath
}

// authorizeURL issues a single-use state bound to the user (and org) and builds the Discord page.
func (s *Server) discordAuthorize(w http.ResponseWriter, r *http.Request, flow, org string) {
	user := userFromContext(r.Context())
	state, err := s.Tickets.Issue(r.Context(), panelcache.ConsoleTicket{UserID: user.ID, Org: org, GameServer: flow}, discordStateTTL)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not start the Discord authorization")
		return
	}
	scopes, perms := []string{discord.ScopeIdentify}, ""
	if flow != flowLink {
		scopes, perms = []string{discord.ScopeBot, discord.ScopeApplicationsCommands}, discord.PermissionsNone
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": s.Bot.Client.AuthorizeURL(scopes, state, s.discordRedirectURI(), perms)})
}

func (s *Server) handleDiscordLinkAuthorize(w http.ResponseWriter, r *http.Request) {
	s.discordAuthorize(w, r, flowLink, "")
}

func (s *Server) handleDiscordUnlink(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if err := s.DB.ClearDiscordLink(r.Context(), user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "discord.unlink", "user", user.Username, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrgDiscordAuthorize(w http.ResponseWriter, r *http.Request) {
	s.discordAuthorize(w, r, flowOrg, orgAccessFromContext(r.Context()).Org.Slug)
}

func (s *Server) handleGetOrgDiscord(w http.ResponseWriter, r *http.Request) {
	_, name, err := s.DB.GetOrgGuild(r.Context(), orgAccessFromContext(r.Context()).Org.ID)
	if err != nil && !errors.Is(err, paneldb.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": err == nil, "guildName": name})
}

func (s *Server) handleOrgDiscordDisconnect(w http.ResponseWriter, r *http.Request) {
	org := orgAccessFromContext(r.Context()).Org
	if err := s.DB.ClearOrgGuild(r.Context(), org.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, org.Slug, "discord.guild.disconnect", "organization", org.Slug, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformDiscordAuthorize(w http.ResponseWriter, r *http.Request) {
	s.discordAuthorize(w, r, flowPlatform, "")
}

func (s *Server) handleGetPlatformDiscord(w http.ResponseWriter, r *http.Request) {
	id, _ := s.DB.GetSetting(r.Context(), settingPlatformGuild)
	name, _ := s.DB.GetSetting(r.Context(), settingPlatformGuildName)
	writeJSON(w, http.StatusOK, map[string]any{"connected": id != "", "guildName": name})
}

func (s *Server) handlePlatformDiscordDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := firstErr(s.DB.DeleteSetting(r.Context(), settingPlatformGuild), s.DB.DeleteSetting(r.Context(), settingPlatformGuildName)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditEvent(r, "", "discord.platform.disconnect", "platform", "discord", "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleDiscordCallback finishes the three OAuth2 flows. It never trusts the browser for who is
// asking: the user (and org) come from the single-use state, and roles are checked again here.
func (s *Server) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.PublicURL, "/")
	back := func(page, result string) {
		http.Redirect(w, r, base+page+"?discord="+url.QueryEscape(result), http.StatusFound)
	}
	st, err := s.Tickets.Consume(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		back("/account", "error")
		return
	}
	page := map[string]string{flowLink: "/account", flowOrg: "/settings", flowPlatform: "/orgs"}[st.GameServer]
	if page == "" || r.URL.Query().Get("code") == "" {
		back("/account", "error")
		return
	}
	user, err := s.DB.GetUser(r.Context(), st.UserID)
	if err != nil {
		back(page, "error")
		return
	}
	tok, err := s.Bot.Client.ExchangeCode(r.Context(), r.URL.Query().Get("code"), s.discordRedirectURI())
	if err != nil {
		discordLog.Error(err, "Discord OAuth2 code exchange failed")
		back(page, "error")
		return
	}
	switch st.GameServer {
	case flowLink:
		me, err := s.Bot.Client.Me(r.Context(), tok.AccessToken)
		if err != nil || me.ID == "" {
			back(page, "error")
			return
		}
		switch err := s.DB.SetDiscordLink(r.Context(), user.ID, me.ID, me.Username); {
		case errors.Is(err, paneldb.ErrAlreadyExists):
			back(page, "taken")
			return
		case err != nil:
			back(page, "error")
			return
		}
		s.auditEventAs(r, user, "", "discord.link", "user", user.Username, "success", map[string]string{"discordUser": me.Username})
		back(page, "linked")
	case flowOrg:
		acc, _, _ := s.resolveOrgAccess(r.Context(), user, st.Org, paneldb.RoleAdmin)
		if acc == nil || tok.Guild == nil {
			back(page, "error")
			return
		}
		switch err := s.DB.SetOrgGuild(r.Context(), acc.Org.ID, tok.Guild.ID, tok.Guild.Name, user.ID); {
		case errors.Is(err, paneldb.ErrAlreadyExists):
			back(page, "taken")
			return
		case err != nil:
			back(page, "error")
			return
		}
		s.auditEventAs(r, user, acc.Org.Slug, "discord.guild.connect", "organization", acc.Org.Slug, "success", map[string]string{"guild": tok.Guild.Name})
		back(page, "connected")
	case flowPlatform:
		if !user.IsAdmin || tok.Guild == nil {
			back(page, "error")
			return
		}
		if err := firstErr(s.DB.SetSetting(r.Context(), settingPlatformGuild, tok.Guild.ID),
			s.DB.SetSetting(r.Context(), settingPlatformGuildName, tok.Guild.Name)); err != nil {
			back(page, "error")
			return
		}
		s.auditEventAs(r, user, "", "discord.platform.connect", "platform", "discord", "success", map[string]string{"guild": tok.Guild.Name})
		guild := tok.Guild.ID
		s.runBackground(func() { s.registerPlatformCommands(context.WithoutCancel(r.Context()), guild) })
		back(page, "connected")
	}
}

func (s *Server) registerPlatformCommands(ctx context.Context, guildID string) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.Bot.Client.RegisterGuildCommands(ctx, guildID, discord.PlatformCommands()); err != nil {
		discordLog.Error(err, "registering the platform's Discord commands failed", "guild", guildID)
	}
}

// RegisterDiscordCommands registers the organization commands globally and the platform commands
// in the platform's guild. Run once at startup.
func (s *Server) RegisterDiscordCommands(ctx context.Context) {
	if !s.discordEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.Bot.Client.RegisterGlobalCommands(ctx, discord.OrgCommands()); err != nil {
		discordLog.Error(err, "registering the Discord commands failed")
	}
	if guild, _ := s.DB.GetSetting(ctx, settingPlatformGuild); guild != "" {
		s.registerPlatformCommands(ctx, guild)
	}
}
