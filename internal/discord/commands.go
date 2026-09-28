package discord

// Command is a slash command definition, registered with Discord.
type Command struct {
	Name                     string            `json:"name"`
	NameLocalizations        map[string]string `json:"name_localizations,omitempty"`
	Description              string            `json:"description"`
	DescriptionLocalizations map[string]string `json:"description_localizations,omitempty"`
	Options                  []CommandOption   `json:"options,omitempty"`
	Contexts                 []int             `json:"contexts"`
	IntegrationTypes         []int             `json:"integration_types"`
}

// CommandOption is one option of a command (strings only, which is all the bot needs).
type CommandOption struct {
	Type                     int               `json:"type"`
	Name                     string            `json:"name"`
	NameLocalizations        map[string]string `json:"name_localizations,omitempty"`
	Description              string            `json:"description"`
	DescriptionLocalizations map[string]string `json:"description_localizations,omitempty"`
	Required                 bool              `json:"required"`
	Autocomplete             bool              `json:"autocomplete,omitempty"`
	MaxLength                int               `json:"max_length,omitempty"`
}

func pt(s string) map[string]string { return map[string]string{"pt-BR": s} }

func command(name, ptName, desc, ptDesc string, opts ...CommandOption) Command {
	return Command{Name: name, NameLocalizations: pt(ptName), Description: desc, DescriptionLocalizations: pt(ptDesc),
		Options: opts, Contexts: []int{contextGuild}, IntegrationTypes: []int{integrationGuildInstall}}
}

func stringOption(name, ptName, desc, ptDesc string, autocomplete bool, maxLen int) CommandOption {
	return CommandOption{Type: OptionString, Name: name, NameLocalizations: pt(ptName), Description: desc,
		DescriptionLocalizations: pt(ptDesc), Required: true, Autocomplete: autocomplete, MaxLength: maxLen}
}

var serverOption = stringOption("server", "servidor", "Game server", "Servidor de jogo", true, 0)

// OrgCommands are registered globally: they work in any guild connected to an organization.
func OrgCommands() []Command {
	return []Command{
		command("servers", "servidores", "List the game servers you can see", "Lista os servidores que você vê"),
		command("status", "status", "Show a game server's status", "Mostra o estado de um servidor", serverOption),
		command("start", "iniciar", "Start a game server", "Liga um servidor", serverOption),
		command("stop", "parar", "Stop a game server", "Desliga um servidor", serverOption),
		command("restart", "reiniciar", "Restart a game server", "Reinicia um servidor", serverOption),
		command("command", "comando", "Send a command to a game server's console", "Envia um comando para o console do servidor",
			serverOption, stringOption("text", "texto", "The console command", "O comando", false, 512)),
		command("backup", "backup", "Back up a game server now", "Faz um backup do servidor agora", serverOption),
	}
}

var orgOption = stringOption("org", "org", "Organization", "Organização", true, 0)

// PlatformCommands are registered only in the platform's guild.
func PlatformCommands() []Command {
	return []Command{
		command("orgs", "orgs", "List organizations", "Lista as organizações"),
		command("suspend", "suspender", "Suspend a game server", "Suspende um servidor", orgOption, serverOption,
			stringOption("reason", "motivo", "Why it is suspended", "Motivo da suspensão", false, 256)),
		command("unsuspend", "reativar", "Lift a suspension", "Reativa um servidor suspenso", orgOption, serverOption),
		command("health", "saude", "Platform health", "Saúde da plataforma"),
	}
}
