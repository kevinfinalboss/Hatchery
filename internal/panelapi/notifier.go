package panelapi

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

const (
	notifyInterval = 15 * time.Second
	// notifyMaxAge keeps a Panel that (re)starts from announcing failures from hours ago.
	notifyMaxAge = 15 * time.Minute
	// discordLocale is the language of Discord messages: the channel is shared and has no profile.
	discordLocale = "pt-BR"
)

var notifyLog = logf.Log.WithName("panelapi-notifier")

// notifyErrors de-duplicates log lines: one per change of error, per organization.
var notifyErrors = struct {
	sync.Mutex
	last map[string]string
}{last: map[string]string{}}

func logNotifyError(org string, err error) {
	notifyErrors.Lock()
	defer notifyErrors.Unlock()
	if err == nil {
		delete(notifyErrors.last, org)
		return
	}
	if notifyErrors.last[org] != err.Error() {
		notifyErrors.last[org] = err.Error()
		notifyLog.Error(err, "notification failed", "org", org)
	}
}

func (s *Server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// RunNotifier checks every organization for things that went wrong until ctx is done.
func (s *Server) RunNotifier(ctx context.Context) {
	t := time.NewTicker(notifyInterval)
	defer t.Stop()
	for {
		s.NotifyOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// NotifyOnce reads what the operator recorded in each organization's namespace and announces
// the occurrences of the last notifyMaxAge that nobody announced yet.
func (s *Server) NotifyOnce(ctx context.Context) {
	if s.Notified == nil {
		return
	}
	var tenants v1.TenantList
	if err := s.Client.List(ctx, &tenants); err != nil {
		logNotifyError("", fmt.Errorf("listing tenants: %w", err))
		return
	}
	now := s.clock()
	for _, tn := range tenants.Items {
		if tn.Status.Phase != v1.TenantPhaseActive || tn.Status.Namespace == "" {
			continue
		}
		org, err := s.DB.GetOrgBySlug(ctx, tn.Name)
		if err != nil {
			continue
		}
		ns := client.InNamespace(tn.Status.Namespace)
		var gss v1.GameServerList
		var bkps v1.GameServerBackupList
		var schs v1.GameServerScheduleList
		if err := firstErr(s.Client.List(ctx, &gss, ns), s.Client.List(ctx, &bkps, ns), s.Client.List(ctx, &schs, ns)); err != nil {
			logNotifyError(org.Slug, fmt.Errorf("listing objects: %w", err))
			continue
		}
		display := map[string]string{}
		for _, gs := range gss.Items {
			display[gs.Name] = serverDisplayName(&gs)
		}
		for _, occ := range detectOccurrences(gss.Items, bkps.Items, schs.Items) {
			if age := now.Sub(occ.At); age > notifyMaxAge || age < -time.Minute {
				continue
			}
			name := display[occ.GameServer]
			if name == "" {
				name = occ.GameServer
			}
			s.notify(ctx, org, occ, name)
		}
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func serverDisplayName(gs *v1.GameServer) string {
	if gs.Spec.DisplayName != "" {
		return gs.Spec.DisplayName
	}
	return gs.Name
}

// notify announces one occurrence unless the org muted its event or it was already claimed
// (by an earlier round or another Panel replica). A failed delivery is not retried: a lost
// alert is acceptable, an alert repeated every 15 seconds by a broken webhook is not.
func (s *Server) notify(ctx context.Context, org *paneldb.Org, occ occurrence, displayName string) {
	if s.Notified == nil {
		return
	}
	settings, err := s.DB.GetNotificationSettings(ctx, org.ID)
	if err != nil {
		logNotifyError(org.Slug, err)
		return
	}
	if slices.Contains(settings.Muted, occ.Event) {
		return
	}
	claimed, err := s.Notified.Claim(ctx, org.Slug+"/"+occ.Identity)
	if err != nil {
		logNotifyError(org.Slug, fmt.Errorf("claiming notification: %w", err))
		return
	}
	if !claimed {
		return
	}
	recipients, err := s.DB.ListAlertRecipients(ctx, org.ID)
	if err != nil {
		logNotifyError(org.Slug, err)
	}
	a := alert{Kind: eventMailKinds[occ.Event], ServerName: displayName, Detail: occ.Detail, At: occ.At,
		Link: s.serverLink(org.Slug, occ.GameServer), Color: colorRed}
	if occ.Event == eventScheduleFailed || occ.Event == eventSuspended {
		a.Color = colorOrange
	}
	discord := s.sendDiscordAlert(ctx, settings.DiscordWebhookURL, org, a)
	email := s.sendAlertMail(ctx, recipients, org, a)
	outcome := "success"
	if strings.HasPrefix(discord, "failed") || strings.HasPrefix(email, "failed") {
		outcome = "failed"
		logNotifyError(org.Slug, fmt.Errorf("%s: discord=%s email=%s", occ.Event, discord, email))
	} else {
		logNotifyError(org.Slug, nil)
	}
	s.writeAudit(ctx, paneldb.AuditEvent{
		OrgSlug: org.Slug, ActorUsername: "system", Action: "notification.sent",
		TargetType: "gameserver", TargetName: occ.GameServer, Outcome: outcome,
		Metadata: sanitizeMeta(map[string]string{"event": occ.Event, "discord": discord, "email": email}),
	})
}

// alert is one message ready to be delivered on every channel.
type alert struct {
	Kind       mailer.Kind
	ServerName string
	Detail     string
	Link       string
	Color      int
	At         time.Time
}

func (s *Server) serverLink(org, name string) string {
	if s.PublicURL == "" || name == "" {
		return ""
	}
	return strings.TrimRight(s.PublicURL, "/") + "/orgs/" + org + "/servers/" + name
}

// sendDiscordAlert posts a to the org's webhook: "sent", "notConfigured" or "failed: <reason>".
func (s *Server) sendDiscordAlert(ctx context.Context, webhook string, org *paneldb.Org, a alert) string {
	if webhook == "" || s.Discord == nil {
		return "notConfigured"
	}
	if !validDiscordWebhook(webhook) {
		return "failed: invalid webhook URL"
	}
	title := mailer.AlertTitle(a.Kind, discordLocale)
	desc := "Organização **" + org.Name + "**"
	if a.Kind == mailer.KindAlertTest {
		title = "Teste de notificações do Hatchery"
		desc += "\nSe esta mensagem chegou, o webhook está funcionando."
	} else {
		title += ": " + a.ServerName
	}
	if a.Detail != "" {
		desc += "\n" + a.Detail
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	if err := s.Discord.Send(ctx, webhook, discordMessage{Title: title, Description: desc, URL: a.Link, Color: a.Color, At: a.At}); err != nil {
		return "failed: " + err.Error()
	}
	return "sent"
}

// sendAlertMail e-mails a to each recipient in their own language: "sent", "notConfigured",
// "noRecipients" or "failed: <n> of <m>".
func (s *Server) sendAlertMail(ctx context.Context, recipients []paneldb.AlertRecipient, org *paneldb.Org, a alert) string {
	if s.Mailer == nil {
		return "notConfigured"
	}
	if len(recipients) == 0 {
		return "noRecipients"
	}
	failed := 0
	var lastErr error
	for _, r := range recipients {
		err := s.sendMail(ctx, r.Email, r.Locale, a.Kind, mailer.Data{OrgName: org.Name, ServerName: a.ServerName, Detail: a.Detail, Link: a.Link})
		if err != nil {
			failed++
			lastErr = err
		}
	}
	if failed > 0 {
		return "failed: " + strconv.Itoa(failed) + " of " + strconv.Itoa(len(recipients)) + " (" + lastErr.Error() + ")"
	}
	return "sent"
}

// notifySuspended announces a suspension from the handler that did it, in the background.
func (s *Server) notifySuspended(ctx context.Context, org *paneldb.Org, gs *v1.GameServer) {
	now := s.clock()
	occ := occurrence{
		Event: eventSuspended, GameServer: gs.Name, Detail: gs.Spec.SuspendReason, At: now,
		Identity: "suspended/" + string(gs.UID) + "/" + strconv.FormatInt(now.UnixNano(), 10),
	}
	ctx = context.WithoutCancel(ctx)
	s.runBackground(func() { s.notify(ctx, org, occ, serverDisplayName(gs)) })
}
