import { serverTitle } from "./gameserver";
import { useMatch, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { useAuth } from "./auth";
import { atLeast, useOrg } from "./org";
import { useRestartServer, useSetServerState } from "./serverActions";
import { useT } from "./i18n";
import { useTheme } from "./theme";

export interface Command {
  id: string;
  label: string;
  group: string;
  hint?: string;
  run: () => void;
}

export function filterCommands(commands: Command[], query: string): Command[] {
  const q = query.trim().toLowerCase();
  if (!q) return commands;
  return commands.filter((c) => c.label.toLowerCase().includes(q) || (c.hint?.toLowerCase().includes(q) ?? false));
}

export function useCommands(): Command[] {
  const navigate = useNavigate();
  const t = useT();
  const { user } = useAuth();
  const { current } = useOrg();
  const { toggle: toggleTheme } = useTheme();
  const setState = useSetServerState();
  const restart = useRestartServer();
  const match = useMatch("/orgs/:org/servers/:name");
  const org = current?.slug ?? "";
  const routeOrg = match?.params.org ?? "";
  const routeName = match?.params.name ?? "";

  const { data: list } = useQuery({
    queryKey: ["gameservers", org],
    queryFn: () => api.listGameServers(org),
    enabled: !!org,
  });
  const { data: server } = useQuery({
    queryKey: ["gameserver", routeOrg, routeName],
    queryFn: () => api.getGameServer(routeOrg, routeName),
    enabled: !!match,
  });

  const commands: Command[] = [];

  if (match && server) {
    const running = server.spec.state === "Running";
    const serverGroup = t("palette.groupServer", { name: routeName });
    const base = `/orgs/${routeOrg}/servers/${routeName}`;
    commands.push({
      id: "server-toggle",
      group: serverGroup,
      label: running ? t("palette.stopServer") : t("palette.startServer"),
      run: () => setState.mutate({ org: routeOrg, name: routeName, state: running ? "Stopped" : "Running" }),
    });
    if (running) {
      commands.push({
        id: "server-restart",
        group: serverGroup,
        label: t("palette.restartServer"),
        run: () => {
          if (confirm(t("server.restartConfirm", { name: routeName }))) restart.mutate({ org: routeOrg, name: routeName });
        },
      });
    }
    commands.push({ id: "server-console", group: serverGroup, label: t("palette.goConsole"), run: () => navigate(`${base}?s=console`) });
    commands.push({ id: "server-metrics", group: serverGroup, label: t("palette.goMetrics"), run: () => navigate(`${base}?s=metrics`) });
    commands.push({ id: "server-files", group: serverGroup, label: t("palette.goFiles"), run: () => navigate(`${base}?s=files`) });
    commands.push({ id: "server-settings", group: serverGroup, label: t("palette.goSettings"), run: () => navigate(`${base}?s=settings`) });
  }

  commands.push({ id: "nav-servers", group: t("palette.groupGoTo"), label: t("palette.navServers"), run: () => navigate("/") });
  if (current) {
    commands.push({ id: "nav-eggs", group: t("palette.groupGoTo"), label: t("palette.navEggs"), run: () => navigate("/eggs") });
    commands.push({ id: "nav-members", group: t("palette.groupGoTo"), label: t("palette.navMembers"), run: () => navigate("/members") });
  }
  if (atLeast(current?.role, "admin")) {
    commands.push({ id: "nav-audit", group: t("palette.groupGoTo"), label: t("palette.navAudit"), run: () => navigate("/audit") });
    commands.push({ id: "nav-settings", group: t("palette.groupGoTo"), label: t("palette.navSettings"), run: () => navigate("/settings") });
  }
  if (user?.isAdmin) {
    commands.push({ id: "nav-orgs", group: t("palette.groupGoTo"), label: t("palette.navOrgs"), run: () => navigate("/orgs") });
    commands.push({ id: "nav-users", group: t("palette.groupGoTo"), label: t("palette.navUsers"), run: () => navigate("/users") });
  }

  for (const s of list?.items ?? []) {
    commands.push({
      id: `server-${s.metadata.name}`,
      group: t("palette.groupServers"),
      label: serverTitle(s),
      hint: s.spec.eggRef.name,
      run: () => navigate(`/orgs/${org}/servers/${s.metadata.name}`),
    });
  }

  commands.push({ id: "toggle-theme", group: t("palette.groupAppearance"), label: t("palette.toggleTheme"), run: toggleTheme });

  return commands;
}
