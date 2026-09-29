package panelapi

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type notificationSettingsResponse struct {
	DiscordConfigured bool     `json:"discordConfigured"`
	Muted             []string `json:"muted"`
	Events            []string `json:"events"`
}

// handleGetNotifications never returns the webhook URL: it is a credential, like the S3 keys.
func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request) {
	org := orgAccessFromContext(r.Context()).Org
	settings, err := s.DB.GetNotificationSettings(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	muted := settings.Muted
	if muted == nil {
		muted = []string{}
	}
	writeJSON(w, http.StatusOK, notificationSettingsResponse{
		DiscordConfigured: settings.DiscordWebhookURL != "", Muted: muted, Events: notificationEvents,
	})
}

// putNotificationsRequest: an omitted discordWebhookUrl keeps the current one, "" removes it.
type putNotificationsRequest struct {
	DiscordWebhookURL *string  `json:"discordWebhookUrl"`
	Muted             []string `json:"muted"`
}

func (s *Server) handlePutNotifications(w http.ResponseWriter, r *http.Request) {
	org := orgAccessFromContext(r.Context()).Org
	var req putNotificationsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if u := req.DiscordWebhookURL; u != nil && *u != "" && !validDiscordWebhook(*u) {
		writeError(w, http.StatusUnprocessableEntity, "discordWebhookUrl must be a Discord webhook URL (https://discord.com/api/webhooks/<id>/<token>)")
		return
	}
	muted := []string{}
	for _, e := range req.Muted {
		if !slices.Contains(notificationEvents, e) {
			writeError(w, http.StatusUnprocessableEntity, "unknown event "+strconv.Quote(e))
			return
		}
		if !slices.Contains(muted, e) {
			muted = append(muted, e)
		}
	}
	discord := "unchanged"
	if u := req.DiscordWebhookURL; u != nil {
		if err := s.DB.SetDiscordWebhook(r.Context(), org.ID, *u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		discord = "set"
		if *u == "" {
			discord = "removed"
		}
	}
	if err := s.DB.SetMutedEvents(r.Context(), org.ID, muted); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mutedJSON, _ := json.Marshal(muted)
	s.auditEvent(r, org.Slug, "notification.settings.update", "organization", org.Slug, "success",
		map[string]string{"discord": discord, "muted": string(mutedJSON)})
	s.handleGetNotifications(w, r)
}

const (
	notificationTestLimit  = 5
	notificationTestWindow = time.Minute
)

// handleTestNotifications sends a test to the org's Discord and an e-mail to the caller only, and
// reports each channel's result: here the error has to reach the person configuring it.
func (s *Server) handleTestNotifications(w http.ResponseWriter, r *http.Request) {
	org := orgAccessFromContext(r.Context()).Org
	user := userFromContext(r.Context())
	if s.RequestLimiter != nil {
		ok, retryAfter, err := s.RequestLimiter.Allow(r.Context(), "notify-test:"+org.Slug, notificationTestLimit, notificationTestWindow)
		if err == nil && !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			writeError(w, http.StatusTooManyRequests, "too many test notifications, try again later")
			return
		}
	}
	settings, err := s.DB.GetNotificationSettings(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a := alert{Kind: mailer.KindAlertTest, Color: colorOrange, At: s.clock()}
	me := []paneldb.AlertRecipient{{Username: user.Username, Email: user.Email, Locale: user.Locale}}
	writeJSON(w, http.StatusOK, map[string]string{
		"discord": s.sendDiscordAlert(r.Context(), settings.DiscordWebhookURL, org, a),
		"email":   s.sendAlertMail(r.Context(), me, org, a),
	})
}
