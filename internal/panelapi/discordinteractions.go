package panelapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kevinfinalboss/Hatchery/internal/discord"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

const (
	maxInteractionBody = 64 << 10
	// commandOutputWait is how long /command waits for the game to answer before showing the log.
	commandOutputWait = 3 * time.Second
	maxCommandLines   = 15
	maxCommandChars   = 1800
	viaDiscord        = "discord"
	// operatorLease is the operator's leader-election Lease (cmd/main.go LeaderElectionID).
	operatorLease = "72bce161.hatchery.io"
)

// handleDiscordInteraction is the endpoint Discord calls for every slash command and autocomplete.
// Nothing is parsed before the signature is verified.
func (s *Server) handleDiscordInteraction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxInteractionBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	if !discord.VerifySignature(s.Bot.PublicKey, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body) {
		writeError(w, http.StatusUnauthorized, "invalid request signature")
		return
	}
	var in discord.Interaction
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid interaction")
		return
	}
	writeJSON(w, http.StatusOK, s.interact(r.Context(), &in))
}

// botText picks the language of the user's Discord client.
func botText(in *discord.Interaction, pt, en string) string {
	if strings.HasPrefix(in.Locale, "pt") {
		return pt
	}
	return en
}

// botContext is who is asking and for which organization (nil org in the platform's guild).
type botContext struct {
	in       *discord.Interaction
	user     *paneldb.User
	org      *paneldb.Org
	platform bool
}

func (s *Server) interact(ctx context.Context, in *discord.Interaction) discord.Response {
	if in.Type == discord.InteractionPing {
		return discord.Response{Type: discord.ResponsePong}
	}
	complete := in.Type == discord.InteractionAutocomplete
	fail := func(msg string) discord.Response {
		if complete {
			return discord.Choices(nil)
		}
		return discord.Ephemeral(msg)
	}
	if in.GuildID == "" {
		return fail(botText(in, "Use os comandos num servidor do Discord conectado ao Hatchery.", "Use the commands in a Discord server connected to Hatchery."))
	}
	bc := &botContext{in: in}
	platformGuild, _ := s.DB.GetSetting(ctx, settingPlatformGuild)
	bc.platform = platformGuild != "" && platformGuild == in.GuildID
	if !bc.platform {
		org, err := s.DB.GetOrgByGuild(ctx, in.GuildID)
		if err != nil {
			return fail(botText(in, "Este servidor do Discord não está conectado a uma organização do Hatchery.", "This Discord server is not connected to a Hatchery organization."))
		}
		bc.org = org
	}
	user, err := s.DB.GetUserByDiscordID(ctx, in.UserID())
	if err != nil {
		link := strings.TrimRight(s.PublicURL, "/") + "/account"
		return fail(botText(in, "Vincule sua conta do Discord em "+link+" para usar os comandos.", "Link your Discord account at "+link+" to use the commands."))
	}
	bc.user = user
	if bc.platform {
		if !user.IsAdmin {
			return fail(botText(in, "Só administradores da plataforma usam estes comandos.", "Only platform administrators can use these commands."))
		}
		if complete {
			return s.platformAutocomplete(ctx, bc)
		}
		return s.platformCommand(ctx, bc)
	}
	if complete {
		return discord.Choices(s.serverChoices(ctx, bc.user, bc.org.Slug, in.Data.String("server")))
	}
	return s.orgCommand(ctx, bc)
}

// call runs one of the Panel's routes as the linked user and turns a failure into a message.
func (s *Server) call(ctx context.Context, bc *botContext, method, path string, body any, out any) (bool, string) {
	code, raw := s.dispatch(ctx, bc.user, viaDiscord, method, path, body)
	if code >= 200 && code < 300 {
		if out != nil {
			_ = json.Unmarshal(raw, out)
		}
		return true, ""
	}
	var e errorResponse
	_ = json.Unmarshal(raw, &e)
	in := bc.in
	switch code {
	case http.StatusNotFound:
		return false, botText(in, "Servidor não encontrado (ou você não tem acesso a ele).", "Server not found (or you have no access to it).")
	case http.StatusForbidden:
		if e.Code == "two_factor_required" {
			return false, botText(in, "Esta organização exige autenticação em dois fatores (2FA). Ative em ",
				"This organization requires two-factor authentication (2FA). Turn it on at ") + s.PublicURL + "/account"
		}
		return false, botText(in, "Você não tem permissão para isso neste servidor.", "You are not allowed to do that on this server.")
	case http.StatusLocked:
		return false, botText(in, "Este servidor está suspenso: ", "This server is suspended: ") + e.Error
	default:
		return false, botText(in, "Não deu certo: ", "That did not work: ") + e.Error
	}
}

func orgPath(org, rest string) string { return "/api/v1/orgs/" + org + rest }

func serverPath(org, name, rest string) string {
	return orgPath(org, "/gameservers/"+url.PathEscape(name)+rest)
}

type botGameServer struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName"`
		State       string `json:"state"`
		Suspended   bool   `json:"suspended"`
	} `json:"spec"`
	Status struct {
		Phase          string `json:"phase"`
		PublicExposure struct {
			Host  string `json:"host"`
			Ports []struct {
				Name string `json:"name"`
				Port int32  `json:"port"`
			} `json:"ports"`
		} `json:"publicExposure"`
	} `json:"status"`
	Players *struct {
		Online int `json:"online"`
		Max    int `json:"max"`
	} `json:"players"`
}

func (b botGameServer) title() string {
	if b.Spec.DisplayName != "" {
		return b.Spec.DisplayName
	}
	return b.Metadata.Name
}

func (b botGameServer) line() string {
	phase := b.Status.Phase
	if phase == "" {
		phase = "Pending"
	}
	out := fmt.Sprintf("**%s** (`%s`) — %s", b.title(), b.Metadata.Name, phase)
	if b.Players != nil {
		out += fmt.Sprintf(" · %d/%d", b.Players.Online, b.Players.Max)
	}
	return out
}

// serverChoices lists the servers the user sees in org, filtered by what was typed.
func (s *Server) serverChoices(ctx context.Context, user *paneldb.User, org, typed string) []discord.Choice {
	code, raw := s.dispatch(ctx, user, viaDiscord, http.MethodGet, orgPath(org, "/gameservers"), nil)
	if code != http.StatusOK {
		return nil
	}
	var list struct {
		Items []botGameServer `json:"items"`
	}
	_ = json.Unmarshal(raw, &list)
	typed = strings.ToLower(typed)
	var out []discord.Choice
	for _, it := range list.Items {
		label := it.title()
		if label != it.Metadata.Name {
			label += " (" + it.Metadata.Name + ")"
		}
		if typed == "" || strings.Contains(strings.ToLower(label), typed) {
			out = append(out, discord.Choice{Name: truncate(label, 100), Value: it.Metadata.Name})
		}
		if len(out) == discord.MaxAutocompleteChoices {
			break
		}
	}
	return out
}

func (s *Server) orgCommand(ctx context.Context, bc *botContext) discord.Response {
	in, org := bc.in, bc.org.Slug
	name := in.Data.String("server")
	done := func(pt, en string) discord.Response { return discord.Ephemeral(botText(in, pt, en)) }
	switch in.Data.Name {
	case "servers":
		var list struct {
			Items []botGameServer `json:"items"`
		}
		if ok, msg := s.call(ctx, bc, http.MethodGet, orgPath(org, "/gameservers"), nil, &list); !ok {
			return discord.Ephemeral(msg)
		}
		if len(list.Items) == 0 {
			return done("Nenhum servidor visível para você.", "No servers visible to you.")
		}
		lines := make([]string, 0, len(list.Items))
		for _, it := range list.Items {
			lines = append(lines, "• "+it.line())
		}
		return discord.Ephemeral(strings.Join(lines, "\n"))
	case "status":
		return discord.Ephemeral(s.statusText(ctx, bc, name))
	case "start", "stop":
		state := map[string]string{"start": "Running", "stop": "Stopped"}[in.Data.Name]
		if ok, msg := s.call(ctx, bc, http.MethodPatch, serverPath(org, name, "/state"), map[string]string{"state": state}, nil); !ok {
			return discord.Ephemeral(msg)
		}
		if state == "Running" {
			return done("Iniciando `"+name+"`.", "Starting `"+name+"`.")
		}
		return done("Parando `"+name+"`.", "Stopping `"+name+"`.")
	case "restart":
		if ok, msg := s.call(ctx, bc, http.MethodPost, serverPath(org, name, "/restart"), nil, nil); !ok {
			return discord.Ephemeral(msg)
		}
		return done("Reiniciando `"+name+"`.", "Restarting `"+name+"`.")
	case "backup":
		var b struct {
			Name string `json:"name"`
		}
		if ok, msg := s.call(ctx, bc, http.MethodPost, serverPath(org, name, "/backups"), nil, &b); !ok {
			return discord.Ephemeral(msg)
		}
		return done("Backup `"+b.Name+"` iniciado.", "Backup `"+b.Name+"` started.")
	case "command":
		since := time.Now().UTC()
		if ok, msg := s.call(ctx, bc, http.MethodPost, serverPath(org, name, "/command"), map[string]string{"command": in.Data.String("text")}, nil); !ok {
			return discord.Ephemeral(msg)
		}
		token := in.Token
		s.runBackground(func() {
			bg := context.WithoutCancel(ctx)
			if s.botWait == nil {
				time.Sleep(commandOutputWait)
			} else {
				s.botWait()
			}
			out := s.commandOutput(bg, bc, org, name, since)
			msg := botText(in, "Comando enviado.", "Command sent.")
			if out != "" {
				msg += "\n```\n" + out + "\n```"
			}
			if err := s.Bot.Client.EditOriginal(bg, token, msg); err != nil {
				discordLog.Error(err, "completing a Discord command response failed")
			}
		})
		return discord.Deferred()
	}
	return done("Comando desconhecido.", "Unknown command.")
}

// commandOutput is what the game printed since the command was sent (log timestamps stripped).
func (s *Server) commandOutput(ctx context.Context, bc *botContext, org, name string, since time.Time) string {
	code, raw := s.dispatch(ctx, bc.user, viaDiscord, http.MethodGet,
		serverPath(org, name, "/logs?sinceTime="+url.QueryEscape(since.Format(time.RFC3339Nano))), nil)
	if code != http.StatusOK {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if ts, rest, ok := strings.Cut(l, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				l = rest
			}
		}
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.ReplaceAll(l, "```", "'''"))
		}
	}
	if len(lines) > maxCommandLines {
		lines = lines[len(lines)-maxCommandLines:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxCommandChars {
		out = out[len(out)-maxCommandChars:]
	}
	return out
}

func (s *Server) statusText(ctx context.Context, bc *botContext, name string) string {
	in, org := bc.in, bc.org.Slug
	var gs botGameServer
	if ok, msg := s.call(ctx, bc, http.MethodGet, serverPath(org, name, ""), nil, &gs); !ok {
		return msg
	}
	lines := []string{gs.line()}
	var rt struct {
		StartedAt *time.Time `json:"startedAt"`
	}
	if ok, _ := s.call(ctx, bc, http.MethodGet, serverPath(org, name, "/runtime"), nil, &rt); ok && rt.StartedAt != nil {
		lines = append(lines, botText(in, "No ar há ", "Up for ")+time.Since(*rt.StartedAt).Round(time.Minute).String())
	}
	var m struct {
		Points []struct {
			CPU int64 `json:"cpuMillicores"`
			Mem int64 `json:"memoryBytes"`
		} `json:"points"`
	}
	if ok, _ := s.call(ctx, bc, http.MethodGet, serverPath(org, name, "/metrics?range=15m"), nil, &m); ok && len(m.Points) > 0 {
		p := m.Points[len(m.Points)-1]
		lines = append(lines, fmt.Sprintf("CPU %dm · %s %d MiB", p.CPU, botText(in, "memória", "memory"), p.Mem>>20))
	}
	for _, p := range gs.Status.PublicExposure.Ports {
		lines = append(lines, fmt.Sprintf("%s: `%s:%d`", p.Name, gs.Status.PublicExposure.Host, p.Port))
	}
	return strings.Join(lines, "\n")
}

func (s *Server) platformAutocomplete(ctx context.Context, bc *botContext) discord.Response {
	in := bc.in
	switch in.Data.Focused() {
	case "org":
		orgs, err := s.DB.ListAllOrgs(ctx)
		if err != nil {
			return discord.Choices(nil)
		}
		typed := strings.ToLower(in.Data.String("org"))
		var out []discord.Choice
		for _, o := range orgs {
			label := o.Name + " (" + o.Slug + ")"
			if typed == "" || strings.Contains(strings.ToLower(label), typed) {
				out = append(out, discord.Choice{Name: truncate(label, 100), Value: o.Slug})
			}
		}
		return discord.Choices(out)
	case "server":
		if org := in.Data.String("org"); org != "" {
			return discord.Choices(s.serverChoices(ctx, bc.user, org, in.Data.String("server")))
		}
	}
	return discord.Choices(nil)
}

func (s *Server) platformCommand(ctx context.Context, bc *botContext) discord.Response {
	in := bc.in
	org, name := in.Data.String("org"), in.Data.String("server")
	switch in.Data.Name {
	case "orgs":
		orgs, err := s.DB.ListAllOrgs(ctx)
		if err != nil {
			return discord.Ephemeral(err.Error())
		}
		sort.Slice(orgs, func(i, j int) bool { return orgs[i].Slug < orgs[j].Slug })
		lines := make([]string, 0, len(orgs))
		for _, o := range orgs {
			lines = append(lines, fmt.Sprintf("• **%s** (`%s`)", o.Name, o.Slug))
		}
		if len(lines) == 0 {
			return discord.Ephemeral(botText(in, "Nenhuma organização.", "No organizations."))
		}
		return discord.Ephemeral(strings.Join(lines, "\n"))
	case "suspend":
		if ok, msg := s.call(ctx, bc, http.MethodPost, serverPath(org, name, "/suspend"), map[string]string{"reason": in.Data.String("reason")}, nil); !ok {
			return discord.Ephemeral(msg)
		}
		return discord.Ephemeral(botText(in, "`"+org+"/"+name+"` suspenso.", "`"+org+"/"+name+"` suspended."))
	case "unsuspend":
		if ok, msg := s.call(ctx, bc, http.MethodPost, serverPath(org, name, "/unsuspend"), nil, nil); !ok {
			return discord.Ephemeral(msg)
		}
		return discord.Ephemeral(botText(in, "`"+org+"/"+name+"` reativado.", "`"+org+"/"+name+"` unsuspended."))
	case "health":
		return discord.Ephemeral(s.healthText(ctx, in))
	}
	return discord.Ephemeral(botText(in, "Comando desconhecido.", "Unknown command."))
}

func (s *Server) healthText(ctx context.Context, in *discord.Interaction) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	check := func(name string, err error) string {
		if err != nil {
			return "🔴 " + name + ": " + err.Error()
		}
		return "🟢 " + name
	}
	lines := []string{
		"🟢 panel-api",
		check("Postgres", s.DB.Ping(ctx)),
	}
	if s.PingCache != nil {
		lines = append(lines, check("Valkey", s.PingCache(ctx)))
	}
	lines = append(lines, s.operatorHealth(ctx, in))
	if s.Clientset != nil {
		if rc := s.Clientset.Discovery().RESTClient(); rc != nil {
			_, err := rc.Get().AbsPath("/apis/metrics.k8s.io/v1beta1").DoRaw(ctx)
			lines = append(lines, check("metrics-server", err))
		}
	}
	return strings.Join(lines, "\n")
}

// operatorHealth reads the operator's leader Lease: a leader renewing it is a live operator.
func (s *Server) operatorHealth(ctx context.Context, in *discord.Interaction) string {
	if s.Clientset == nil || s.PodNamespace == "" {
		return "⚪ operator"
	}
	lease, err := s.Clientset.CoordinationV1().Leases(s.PodNamespace).Get(ctx, operatorLease, metav1.GetOptions{})
	if err != nil {
		return "🔴 operator: " + err.Error()
	}
	if lease.Spec.RenewTime == nil {
		return "🔴 operator: " + botText(in, "sem líder", "no leader")
	}
	age := time.Since(lease.Spec.RenewTime.Time).Round(time.Second)
	if age > time.Minute {
		return "🔴 operator: " + botText(in, "líder não renova há ", "leader not renewed for ") + age.String()
	}
	return "🟢 operator (" + botText(in, "renovado há ", "renewed ") + age.String() + ")"
}
