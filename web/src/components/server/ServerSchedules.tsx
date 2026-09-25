import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { useI18n, type TKey } from "../../lib/i18n";
import { errorMessage } from "../../lib/errors";
import { cronToPreset, presetToCron, timeOf, weekdayName, type Preset } from "../../lib/cron";
import type { GameServer, ScheduleAction, ScheduleItem, ScheduleTask, ScheduleWrite } from "../../lib/types";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";
import { Badge, ListRow, RowActions } from "../ui/List";

const MAX_TASKS = 10;
const MAX_SCHEDULES = 10;
const ACTIONS: ScheduleAction[] = ["Command", "Restart", "Start", "Stop", "Backup"];

const RESULT_KEY: Record<string, TKey> = {
  Succeeded: "schedules.resultSucceeded",
  Failed: "schedules.resultFailed",
  Skipped: "schedules.resultSkipped",
};

const ACTION_KEY: Record<ScheduleAction, TKey> = {
  Command: "schedules.actionCommand",
  Restart: "schedules.actionRestart",
  Start: "schedules.actionStart",
  Stop: "schedules.actionStop",
  Backup: "schedules.actionBackup",
};

function selectClass(disabled?: boolean) {
  return `rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary ${disabled ? "opacity-50" : ""}`;
}

function supportedTimeZones(): string[] {
  try {
    const withZones = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
    return withZones.supportedValuesOf ? withZones.supportedValuesOf("timeZone") : [];
  } catch {
    return [];
  }
}

function describeCron(cron: string, t: (key: TKey, vars?: Record<string, string | number>) => string, locale: string): string {
  const p = cronToPreset(cron);
  switch (p.kind) {
    case "daily":
      return t("schedules.descDaily", { time: timeOf(p) });
    case "everyHours":
      return t("schedules.descEveryHours", { n: p.hours });
    case "weekly":
      return t("schedules.descWeekly", { weekday: weekdayName(p.weekday, locale), time: timeOf(p) });
    case "custom":
      return p.cron;
  }
}

function defaultTask(): ScheduleTask {
  return { action: "Command", command: "", delaySeconds: 0 };
}

function toWrite(item: ScheduleItem, displayName: string, cron: string, timeZone: string, onlyWhenRunning: boolean, tasks: ScheduleTask[]): ScheduleWrite {
  return { displayName: displayName.trim(), cron, timeZone, suspend: item.spec.suspend ?? false, onlyWhenRunning, tasks };
}

function ScheduleEditor({
  org,
  name,
  editing,
  item,
  onClose,
}: {
  org: string;
  name: string;
  editing: string; // "" for a new schedule, otherwise the schedule name being edited
  item?: ScheduleItem;
  onClose: () => void;
}) {
  const { t, locale, timeZone: profileTimeZone } = useI18n();
  const queryClient = useQueryClient();
  const isNew = editing === "";

  const [displayName, setDisplayName] = useState(item?.spec.displayName ?? "");
  const [preset, setPreset] = useState<Preset>(item ? cronToPreset(item.spec.cron) : { kind: "daily", hour: 3, minute: 0 });
  const [timeZone, setTimeZone] = useState(item?.spec.timeZone || profileTimeZone || Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [onlyWhenRunning, setOnlyWhenRunning] = useState(item?.spec.onlyWhenRunning ?? true);
  const [tasks, setTasks] = useState<ScheduleTask[]>(item && item.spec.tasks.length > 0 ? item.spec.tasks : [defaultTask()]);
  const [error, setError] = useState<string | null>(null);

  const timezones = useMemo(() => supportedTimeZones(), []);
  const cron = presetToCron(preset);

  const updateTask = (i: number, patch: Partial<ScheduleTask>) => setTasks((ts) => ts.map((task, idx) => (idx === i ? { ...task, ...patch } : task)));
  const setTaskAction = (i: number, action: ScheduleAction) =>
    setTasks((ts) =>
      ts.map((task, idx) =>
        idx !== i
          ? task
          : {
              action,
              command: action === "Command" ? (task.command ?? "") : undefined,
              delaySeconds: task.delaySeconds,
              keepLast: action === "Backup" ? (task.keepLast ?? 0) : undefined,
            },
      ),
    );
  const moveTask = (i: number, dir: -1 | 1) =>
    setTasks((ts) => {
      const j = i + dir;
      if (j < 0 || j >= ts.length) return ts;
      const next = [...ts];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  const removeTask = (i: number) => setTasks((ts) => (ts.length > 1 ? ts.filter((_, idx) => idx !== i) : ts));
  const addTask = () => setTasks((ts) => (ts.length >= MAX_TASKS ? ts : [...ts, defaultTask()]));

  const valid = cron.trim() !== "" && tasks.every((task) => task.action !== "Command" || (task.command ?? "").trim() !== "");

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["schedules", org, name] });
  };

  const save = useMutation({
    mutationFn: () => {
      const body: ScheduleWrite = isNew
        ? { displayName: displayName.trim(), cron, timeZone, suspend: false, onlyWhenRunning, tasks }
        : toWrite(item!, displayName, cron, timeZone, onlyWhenRunning, tasks);
      return isNew ? api.createSchedule(org, name, body) : api.updateSchedule(org, name, editing, body);
    },
    onSuccess: () => {
      refresh();
      onClose();
    },
    onError: (err) => setError(errorMessage(err, t("schedules.saveFailed"))),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">
        {isNew ? t("schedules.new") : t("schedules.editTitle", { name: item?.spec.displayName || editing })}
      </div>

      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          save.mutate();
        }}
      >
        <Field label={t("schedules.nameLabel")} htmlFor="sch-name">
          <Input id="sch-name" value={displayName} maxLength={64} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>

        <Field label={t("schedules.frequencyLabel")} htmlFor="sch-preset">
          <select
            id="sch-preset"
            value={preset.kind}
            onChange={(e) => {
              const kind = e.target.value as Preset["kind"];
              if (kind === "daily") setPreset({ kind: "daily", hour: 3, minute: 0 });
              else if (kind === "everyHours") setPreset({ kind: "everyHours", hours: 6 });
              else if (kind === "weekly") setPreset({ kind: "weekly", weekday: 0, hour: 3, minute: 0 });
              else setPreset({ kind: "custom", cron });
            }}
            className={selectClass()}
          >
            <option value="daily">{t("schedules.presetDaily")}</option>
            <option value="everyHours">{t("schedules.presetEveryHours")}</option>
            <option value="weekly">{t("schedules.presetWeekly")}</option>
            <option value="custom">{t("schedules.presetCustom")}</option>
          </select>
        </Field>

        {preset.kind === "daily" && (
          <Field label={t("schedules.timeLabel")} htmlFor="sch-time">
            <input
              id="sch-time"
              type="time"
              value={timeOf(preset)}
              onChange={(e) => {
                const [h, m] = e.target.value.split(":").map(Number);
                if (Number.isFinite(h) && Number.isFinite(m)) setPreset({ kind: "daily", hour: h, minute: m });
              }}
              className={selectClass()}
            />
          </Field>
        )}

        {preset.kind === "everyHours" && (
          <Field label={t("schedules.everyHoursLabel")} htmlFor="sch-hours">
            <select
              id="sch-hours"
              value={preset.hours}
              onChange={(e) => setPreset({ kind: "everyHours", hours: Number(e.target.value) as 1 | 2 | 3 | 4 | 6 | 8 | 12 })}
              className={selectClass()}
            >
              {[1, 2, 3, 4, 6, 8, 12].map((h) => (
                <option key={h} value={h}>
                  {t("schedules.everyHoursOption", { n: h })}
                </option>
              ))}
            </select>
          </Field>
        )}

        {preset.kind === "weekly" && (
          <div className="flex flex-wrap gap-3">
            <Field label={t("schedules.weekdayLabel")} htmlFor="sch-weekday">
              <select
                id="sch-weekday"
                value={preset.weekday}
                onChange={(e) => setPreset({ kind: "weekly", weekday: Number(e.target.value), hour: preset.hour, minute: preset.minute })}
                className={selectClass()}
              >
                {[0, 1, 2, 3, 4, 5, 6].map((d) => (
                  <option key={d} value={d}>
                    {weekdayName(d, locale)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t("schedules.timeLabel")} htmlFor="sch-week-time">
              <input
                id="sch-week-time"
                type="time"
                value={timeOf(preset)}
                onChange={(e) => {
                  const [h, m] = e.target.value.split(":").map(Number);
                  if (Number.isFinite(h) && Number.isFinite(m)) setPreset({ kind: "weekly", weekday: preset.weekday, hour: h, minute: m });
                }}
                className={selectClass()}
              />
            </Field>
          </div>
        )}

        {preset.kind === "custom" && (
          <Field label={t("schedules.cronLabel")} htmlFor="sch-cron">
            <Input
              id="sch-cron"
              className="font-mono text-xs"
              value={preset.cron}
              placeholder="*/15 * * * *"
              onChange={(e) => setPreset({ kind: "custom", cron: e.target.value })}
            />
            <span className="font-prose text-xs text-text-tertiary">{t("schedules.cronHint")}</span>
          </Field>
        )}

        <Field label={t("schedules.timezoneLabel")} htmlFor="sch-tz">
          {timezones.length > 0 ? (
            <select id="sch-tz" value={timeZone} onChange={(e) => setTimeZone(e.target.value)} className={selectClass()}>
              {timezones.map((z) => (
                <option key={z} value={z}>
                  {z}
                </option>
              ))}
            </select>
          ) : (
            <Input id="sch-tz" value={timeZone} onChange={(e) => setTimeZone(e.target.value)} />
          )}
        </Field>

        <label className="flex items-center gap-2 font-prose text-sm text-text-primary">
          <input type="checkbox" checked={onlyWhenRunning} onChange={(e) => setOnlyWhenRunning(e.target.checked)} />
          {t("schedules.onlyWhenRunning")}
        </label>

        <div>
          <div className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("schedules.tasksTitle")}</div>
          <p className="font-prose text-xs text-text-tertiary">{t("schedules.tasksHint")}</p>
        </div>

        <div className="flex flex-col gap-3">
          {tasks.map((task, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2 border border-border p-3">
              <Field label={t("schedules.taskAction")} htmlFor={`sch-task-${i}-action`}>
                <select
                  id={`sch-task-${i}-action`}
                  value={task.action}
                  onChange={(e) => setTaskAction(i, e.target.value as ScheduleAction)}
                  className={selectClass()}
                >
                  {ACTIONS.map((a) => (
                    <option key={a} value={a}>
                      {t(ACTION_KEY[a])}
                    </option>
                  ))}
                </select>
              </Field>

              {task.action === "Command" && (
                <Field label={t("schedules.commandLabel")} htmlFor={`sch-task-${i}-cmd`}>
                  <Input
                    id={`sch-task-${i}-cmd`}
                    className="min-w-[16rem] font-mono text-xs"
                    value={task.command ?? ""}
                    maxLength={512}
                    required
                    onChange={(e) => updateTask(i, { command: e.target.value })}
                  />
                </Field>
              )}

              <Field label={t("schedules.delayLabel")} htmlFor={`sch-task-${i}-delay`}>
                <Input
                  id={`sch-task-${i}-delay`}
                  type="number"
                  min={0}
                  max={900}
                  className="w-24"
                  value={task.delaySeconds ?? 0}
                  onChange={(e) => updateTask(i, { delaySeconds: Number(e.target.value) })}
                />
              </Field>

              {task.action === "Backup" && (
                <Field label={t("schedules.keepLastLabel")} htmlFor={`sch-task-${i}-keep`}>
                  <Input
                    id={`sch-task-${i}-keep`}
                    type="number"
                    min={0}
                    max={100}
                    className="w-24"
                    value={task.keepLast ?? 0}
                    onChange={(e) => updateTask(i, { keepLast: Number(e.target.value) })}
                  />
                </Field>
              )}

              <div className="flex gap-1">
                <Button type="button" variant="ghost" disabled={i === 0} title={t("schedules.moveUp")} onClick={() => moveTask(i, -1)}>
                  ↑
                </Button>
                <Button type="button" variant="ghost" disabled={i === tasks.length - 1} title={t("schedules.moveDown")} onClick={() => moveTask(i, 1)}>
                  ↓
                </Button>
                <Button type="button" variant="ghost" disabled={tasks.length <= 1} title={t("schedules.removeTask")} onClick={() => removeTask(i)}>
                  ✕
                </Button>
              </div>
            </div>
          ))}
          <div>
            <Button type="button" variant="secondary" disabled={tasks.length >= MAX_TASKS} onClick={addTask}>
              {t("schedules.addTask")}
            </Button>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <Button type="submit" disabled={!valid || save.isPending}>
            {save.isPending ? t("schedules.saving") : t("schedules.save")}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("schedules.cancel")}
          </Button>
          {error && <span className="font-sans text-sm text-status-failed">{error}</span>}
        </div>
      </form>
    </Card>
  );
}

function ScheduleRow({
  item,
  onEdit,
  onDelete,
  onToggleSuspend,
  onRun,
  busy,
}: {
  item: ScheduleItem;
  onEdit: () => void;
  onDelete: () => void;
  onToggleSuspend: () => void;
  onRun: () => void;
  busy: boolean;
}) {
  const { t, locale, formatDateTime } = useI18n();
  const lastRun = item.status.lastRun;
  const disruptive = item.spec.tasks.some((task) => task.action === "Restart" || task.action === "Stop");

  return (
    <ListRow>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate font-sans text-sm text-text-primary">{item.spec.displayName || t("schedules.unnamed")}</span>
          {item.spec.suspend && <Badge>{t("schedules.paused")}</Badge>}
          {item.status.activeRun && <Badge>{t("schedules.runningBadge")}</Badge>}
        </div>
        <div className="mt-0.5 font-sans text-xs text-text-tertiary">
          {describeCron(item.spec.cron, t, locale)} ({item.spec.timeZone || "UTC"})
        </div>
        <div className="mt-0.5 font-sans text-xs text-text-tertiary">
          {t("schedules.nextRun")}: {item.status.nextScheduleTime ? formatDateTime(item.status.nextScheduleTime) : "—"}
          {" · "}
          {t("schedules.lastRun")}:{" "}
          {lastRun ? (
            <span title={lastRun.message} className={lastRun.result === "Failed" ? "text-status-failed" : undefined}>
              {t(RESULT_KEY[lastRun.result] ?? "schedules.resultSkipped")} ({formatDateTime(lastRun.startedAt)})
            </span>
          ) : (
            "—"
          )}
        </div>
      </div>
      <RowActions>
        <Button
          variant="secondary"
          disabled={busy}
          onClick={() => {
            if (!disruptive || confirm(t("schedules.runNowConfirm", { name: item.spec.displayName || item.name }))) onRun();
          }}
        >
          {t("schedules.runNow")}
        </Button>
        <Button variant="ghost" disabled={busy} onClick={onToggleSuspend}>
          {item.spec.suspend ? t("schedules.resume") : t("schedules.pause")}
        </Button>
        <Button variant="ghost" disabled={busy} onClick={onEdit}>
          {t("schedules.edit")}
        </Button>
        <Button
          variant="ghost"
          disabled={busy}
          onClick={() => {
            if (confirm(t("schedules.deleteConfirm", { name: item.spec.displayName || item.name }))) onDelete();
          }}
        >
          {t("schedules.delete")}
        </Button>
      </RowActions>
    </ListRow>
  );
}

export function ServerSchedules({ org, name }: { org: string; name: string; server: GameServer }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data: items, error: listError } = useQuery({
    queryKey: ["schedules", org, name],
    queryFn: () => api.listSchedules(org, name),
    refetchInterval: 10000,
  });

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["schedules", org, name] });
  const onError = (err: unknown) => setError(errorMessage(err, t("schedules.actionFailed")));

  const remove = useMutation({ mutationFn: (schedule: string) => api.deleteSchedule(org, name, schedule), onSuccess: refresh, onError });
  const run = useMutation({ mutationFn: (schedule: string) => api.runSchedule(org, name, schedule), onSuccess: refresh, onError });
  const toggle = useMutation({
    mutationFn: (item: ScheduleItem) =>
      api.updateSchedule(org, name, item.name, {
        displayName: item.spec.displayName ?? "",
        cron: item.spec.cron,
        timeZone: item.spec.timeZone ?? "",
        suspend: !item.spec.suspend,
        onlyWhenRunning: item.spec.onlyWhenRunning,
        tasks: item.spec.tasks,
      }),
    onSuccess: refresh,
    onError,
  });
  const busy = remove.isPending || run.isPending || toggle.isPending;

  const list = items ?? [];
  const editingItem = editing ? list.find((i) => i.name === editing) : undefined;

  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <Card className="divide-y divide-border">
        {listError && <div className="px-4 py-3 font-sans text-sm text-status-failed">{errorMessage(listError, t("schedules.loadFailed"))}</div>}
        {!listError && list.length === 0 && <div className="px-4 py-3 font-prose text-sm text-text-tertiary">{t("schedules.empty")}</div>}
        {list.map((item) => (
          <ScheduleRow
            key={item.name}
            item={item}
            busy={busy}
            onEdit={() => setEditing(item.name)}
            onDelete={() => remove.mutate(item.name)}
            onToggleSuspend={() => toggle.mutate(item)}
            onRun={() => run.mutate(item.name)}
          />
        ))}
      </Card>

      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}

      {editing === null && (
        <div>
          <Button variant="secondary" disabled={list.length >= MAX_SCHEDULES} onClick={() => setEditing("")}>
            {t("schedules.new")}
          </Button>
          {list.length >= MAX_SCHEDULES && <p className="mt-1 font-prose text-xs text-text-tertiary">{t("schedules.maxReached", { max: MAX_SCHEDULES })}</p>}
        </div>
      )}

      {editing !== null && <ScheduleEditor org={org} name={name} editing={editing} item={editingItem} onClose={() => setEditing(null)} />}
    </div>
  );
}
