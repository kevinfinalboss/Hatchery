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

package mailer

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

// Kind names one of the Panel's e-mails.
type Kind string

const (
	KindInvitation        Kind = "invitation"
	KindPasswordReset     Kind = "password_reset"
	KindEmailChange       Kind = "email_change"
	KindEmailChangeNotice Kind = "email_change_notice"

	// Alerts sent to an organization's owners and admins (see internal/panelapi/notifier.go).
	KindAlertGaveUp         Kind = "alert_gave_up"
	KindAlertBackupFailed   Kind = "alert_backup_failed"
	KindAlertResumeFailed   Kind = "alert_resume_failed"
	KindAlertScheduleFailed Kind = "alert_schedule_failed"
	KindAlertSuspended      Kind = "alert_suspended"
	KindAlertTest           Kind = "alert_test"
)

// Data fills the templates; each kind uses a subset.
type Data struct {
	Link      string
	OrgName   string
	Role      string
	InvitedBy string
	Username  string
	NewEmail  string
	// ServerName and Detail describe an alert: the server's display name and what went wrong.
	ServerName string
	Detail     string
}

type tpl struct{ subject, text, html string }

const defaultLocale = "pt-BR"

// layout wraps every HTML body; {{.Body}} is already-rendered, safe HTML.
const layout = `<!doctype html><html><body style="margin:0;padding:24px;background:#f4f4f5;font-family:Arial,Helvetica,sans-serif;color:#18181b">` +
	`<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e4e4e7;padding:24px">` +
	`<div style="font-family:monospace;font-size:18px;font-weight:bold;margin-bottom:16px">hatchery_</div>{{.Body}}</div></body></html>`

const button = `<p style="margin:24px 0"><a href="{{.Link}}" style="background:#a3e635;color:#09090b;padding:10px 16px;text-decoration:none;font-weight:bold">`

var templates = map[string]map[Kind]tpl{
	"pt-BR": {
		KindInvitation: {
			subject: "Convite para a organização {{.OrgName}} no Hatchery",
			text:    "{{.InvitedBy}} convidou você para a organização {{.OrgName}} no Hatchery, com o papel {{.Role}}.\n\nAceite o convite em:\n{{.Link}}\n\nO link vale por 7 dias. Se você não esperava este convite, ignore este e-mail.\n",
			html:    `<p><b>{{.InvitedBy}}</b> convidou você para a organização <b>{{.OrgName}}</b> no Hatchery, com o papel <b>{{.Role}}</b>.</p>` + button + `Aceitar convite</a></p><p style="color:#71717a;font-size:13px">O link vale por 7 dias. Se você não esperava este convite, ignore este e-mail.</p>`,
		},
		KindPasswordReset: {
			subject: "Redefinir sua senha do Hatchery",
			text:    "Olá, {{.Username}}.\n\nRecebemos um pedido para redefinir sua senha. Crie uma nova em:\n{{.Link}}\n\nO link vale por 1 hora. Se não foi você, ignore este e-mail: sua senha continua a mesma.\n",
			html:    `<p>Olá, {{.Username}}.</p><p>Recebemos um pedido para redefinir sua senha.</p>` + button + `Criar nova senha</a></p><p style="color:#71717a;font-size:13px">O link vale por 1 hora. Se não foi você, ignore este e-mail: sua senha continua a mesma.</p>`,
		},
		KindEmailChange: {
			subject: "Confirme seu novo e-mail no Hatchery",
			text:    "Olá, {{.Username}}.\n\nPara usar este endereço na sua conta do Hatchery, confirme em:\n{{.Link}}\n\nO link vale por 24 horas. Se não foi você, ignore este e-mail.\n",
			html:    `<p>Olá, {{.Username}}.</p><p>Para usar este endereço na sua conta do Hatchery, confirme abaixo.</p>` + button + `Confirmar e-mail</a></p><p style="color:#71717a;font-size:13px">O link vale por 24 horas. Se não foi você, ignore este e-mail.</p>`,
		},
		KindEmailChangeNotice: {
			subject: "Pedido de troca de e-mail no Hatchery",
			text:    "Olá, {{.Username}}.\n\nFoi pedida a troca do e-mail da sua conta para {{.NewEmail}}. A troca só vale depois de confirmada pelo novo endereço.\n\nSe não foi você, troque sua senha agora.\n",
			html:    `<p>Olá, {{.Username}}.</p><p>Foi pedida a troca do e-mail da sua conta para <b>{{.NewEmail}}</b>. A troca só vale depois de confirmada pelo novo endereço.</p><p>Se não foi você, troque sua senha agora.</p>`,
		},
	},
	"en": {
		KindInvitation: {
			subject: "Invitation to the {{.OrgName}} organization on Hatchery",
			text:    "{{.InvitedBy}} invited you to the {{.OrgName}} organization on Hatchery, as {{.Role}}.\n\nAccept the invitation at:\n{{.Link}}\n\nThe link is valid for 7 days. If you were not expecting this, ignore this e-mail.\n",
			html:    `<p><b>{{.InvitedBy}}</b> invited you to the <b>{{.OrgName}}</b> organization on Hatchery, as <b>{{.Role}}</b>.</p>` + button + `Accept invitation</a></p><p style="color:#71717a;font-size:13px">The link is valid for 7 days. If you were not expecting this, ignore this e-mail.</p>`,
		},
		KindPasswordReset: {
			subject: "Reset your Hatchery password",
			text:    "Hi {{.Username}},\n\nWe received a request to reset your password. Choose a new one at:\n{{.Link}}\n\nThe link is valid for 1 hour. If it wasn't you, ignore this e-mail: your password stays the same.\n",
			html:    `<p>Hi {{.Username}},</p><p>We received a request to reset your password.</p>` + button + `Choose a new password</a></p><p style="color:#71717a;font-size:13px">The link is valid for 1 hour. If it wasn't you, ignore this e-mail: your password stays the same.</p>`,
		},
		KindEmailChange: {
			subject: "Confirm your new Hatchery e-mail",
			text:    "Hi {{.Username}},\n\nTo use this address on your Hatchery account, confirm at:\n{{.Link}}\n\nThe link is valid for 24 hours. If it wasn't you, ignore this e-mail.\n",
			html:    `<p>Hi {{.Username}},</p><p>To use this address on your Hatchery account, confirm below.</p>` + button + `Confirm e-mail</a></p><p style="color:#71717a;font-size:13px">The link is valid for 24 hours. If it wasn't you, ignore this e-mail.</p>`,
		},
		KindEmailChangeNotice: {
			subject: "E-mail change requested on Hatchery",
			text:    "Hi {{.Username}},\n\nSomeone asked to change your account's e-mail to {{.NewEmail}}. It only takes effect once confirmed from the new address.\n\nIf it wasn't you, change your password now.\n",
			html:    `<p>Hi {{.Username}},</p><p>Someone asked to change your account's e-mail to <b>{{.NewEmail}}</b>. It only takes effect once confirmed from the new address.</p><p>If it wasn't you, change your password now.</p>`,
		},
	},
}

// alertTitles are the headlines of each alert, per locale.
var alertTitles = map[string]map[Kind]string{
	"pt-BR": {
		KindAlertGaveUp:         "Servidor parado após falhas",
		KindAlertBackupFailed:   "Backup falhou",
		KindAlertResumeFailed:   "Salvamento do mundo não foi religado",
		KindAlertScheduleFailed: "Agendamento falhou",
		KindAlertSuspended:      "Servidor suspenso",
	},
	"en": {
		KindAlertGaveUp:         "Server stopped after crashing",
		KindAlertBackupFailed:   "Backup failed",
		KindAlertResumeFailed:   "World saving was not turned back on",
		KindAlertScheduleFailed: "Schedule failed",
		KindAlertSuspended:      "Server suspended",
	},
}

// alertTemplates builds the alert e-mails of one locale from alertTitles: they all share one layout.
func alertTemplates(title map[Kind]string, org, open, testSubject, testBody string) map[Kind]tpl {
	out := map[Kind]tpl{}
	for kind, t := range title {
		out[kind] = tpl{
			subject: "[Hatchery] " + t + ": {{.ServerName}}",
			text:    t + "\n\n{{.ServerName}} (" + org + " {{.OrgName}})\n{{.Detail}}\n{{if .Link}}\n" + open + ": {{.Link}}\n{{end}}",
			html: `<p style="font-size:16px;font-weight:bold">` + t + `</p><p><b>{{.ServerName}}</b> (` + org + ` {{.OrgName}})</p>` +
				`<p style="color:#3f3f46">{{.Detail}}</p>{{if .Link}}` + button + open + `</a></p>{{end}}`,
		}
	}
	out[KindAlertTest] = tpl{
		subject: "[Hatchery] " + testSubject + " — {{.OrgName}}",
		text:    testBody + "\n\n" + org + " {{.OrgName}}\n",
		html:    `<p>` + testBody + `</p><p>` + org + ` <b>{{.OrgName}}</b></p>`,
	}
	return out
}

func init() {
	for kind, t := range alertTemplates(alertTitles["pt-BR"], "organização", "Abrir no painel",
		"Teste de notificações", "Este é um teste das notificações do Hatchery: se você recebeu, o e-mail está funcionando.") {
		templates["pt-BR"][kind] = t
	}
	for kind, t := range alertTemplates(alertTitles["en"], "organization", "Open in the panel",
		"Notification test", "This is a test of Hatchery's notifications: if you got it, e-mail works.") {
		templates["en"][kind] = t
	}
}

// AlertTitle is the localized headline of an alert kind (pt-BR when the locale is unknown).
func AlertTitle(kind Kind, locale string) string {
	set, ok := alertTitles[locale]
	if !ok {
		set = alertTitles[defaultLocale]
	}
	return set[kind]
}

// Render builds the subject and bodies of kind in locale (pt-BR when the
// locale is empty or unknown). HTML values are escaped by html/template.
func Render(kind Kind, locale string, d Data) (Message, error) {
	set, ok := templates[locale]
	if !ok {
		set = templates[defaultLocale]
	}
	t, ok := set[kind]
	if !ok {
		return Message{}, fmt.Errorf("unknown e-mail kind %q", kind)
	}
	subject, err := execText(t.subject, d)
	if err != nil {
		return Message{}, err
	}
	text, err := execText(t.text, d)
	if err != nil {
		return Message{}, err
	}
	body, err := execHTML(t.html, d)
	if err != nil {
		return Message{}, err
	}
	html, err := execHTML(layout, struct{ Body htmltemplate.HTML }{htmltemplate.HTML(body)}) //nolint:gosec // body was rendered by html/template above
	if err != nil {
		return Message{}, err
	}
	return Message{Subject: subject, Text: text, HTML: html}, nil
}

func execText(src string, d any) (string, error) {
	t, err := texttemplate.New("t").Parse(src)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = t.Execute(&b, d)
	return b.String(), err
}

func execHTML(src string, d any) (string, error) {
	t, err := htmltemplate.New("t").Parse(src)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = t.Execute(&b, d)
	return b.String(), err
}
