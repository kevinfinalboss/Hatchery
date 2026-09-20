import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { api, getAuthToken } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { useTheme } from "../../lib/theme";
import { readXtermTheme } from "../../lib/xtermTheme";
import { Input } from "../ui/Input";
import { Button } from "../ui/Button";

const RETRY_MS = 2000;

type StreamState = "idle" | "connecting" | "live" | "reconnecting";

function sleep(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}

export function ServerConsole({ org, name, running }: { org: string; name: string; running: boolean }) {
  const t = useT();
  const { theme } = useTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const historyRef = useRef<string[]>([]);
  const historyIndexRef = useRef(0);

  const [stream, setStream] = useState<StreamState>("idle");
  const [connected, setConnected] = useState(false);
  const [command, setCommand] = useState("");

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = new Terminal({
      convertEol: true,
      disableStdin: true,
      scrollback: 20000,
      fontFamily: "'JetBrains Mono', monospace",
      fontSize: 13,
      theme: readXtermTheme(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(container);
    fit.fit();
    termRef.current = term;

    const resizeObserver = new ResizeObserver(() => fit.fit());
    resizeObserver.observe(container);

    return () => {
      resizeObserver.disconnect();
      term.dispose();
      termRef.current = null;
    };
  }, [org, name]);

  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = readXtermTheme();
  }, [theme]);

  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    if (!running) {
      setStream("idle");
      return;
    }

    const controller = new AbortController();
    let first = true;

    void (async () => {
      while (!controller.signal.aborted) {
        setStream(first ? "connecting" : "reconnecting");
        try {
          const token = getAuthToken();
          const res = await fetch(api.logsPath(org, name), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            signal: controller.signal,
          });
          if (!res.ok || !res.body) throw new Error(`logs ${res.status}`);

          term.reset();
          setStream("live");
          const reader = res.body.getReader();
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            term.write(value);
          }
        } catch {
        }
        if (controller.signal.aborted) return;
        first = false;
        await sleep(RETRY_MS, controller.signal);
      }
    })();

    return () => controller.abort();
  }, [org, name, running]);

  useEffect(() => {
    if (!running) {
      setConnected(false);
      return;
    }

    const controller = new AbortController();

    void (async () => {
      while (!controller.signal.aborted) {
        try {
          const { ticket } = await api.consoleTicket(org, name);
          if (controller.signal.aborted) return;

          const socket = new WebSocket(api.consoleUrl(org, name, ticket));
          wsRef.current = socket;
          await new Promise<void>((resolve) => {
            socket.onopen = () => setConnected(true);
            socket.onclose = () => resolve();
            socket.onerror = () => resolve();
          });
          setConnected(false);
        } catch {
          // ticket falhou: tenta de novo
        }
        if (controller.signal.aborted) return;
        await sleep(RETRY_MS, controller.signal);
      }
    })();

    return () => {
      controller.abort();
      wsRef.current?.close();
      wsRef.current = null;
      setConnected(false);
    };
  }, [org, name, running]);

  function sendCommand() {
    const trimmed = command.trim();
    const ws = wsRef.current;
    const term = termRef.current;
    if (!trimmed || !term || !ws || ws.readyState !== WebSocket.OPEN) return;

    term.write(`\x1b[32m> ${trimmed}\x1b[0m\r\n`);
    ws.send(trimmed + "\n");

    const history = historyRef.current;
    if (history[history.length - 1] !== trimmed) history.push(trimmed);
    historyIndexRef.current = history.length;
    setCommand("");
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    const history = historyRef.current;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (history.length === 0) return;
      historyIndexRef.current = Math.max(0, historyIndexRef.current - 1);
      setCommand(history[historyIndexRef.current]);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      if (historyIndexRef.current >= history.length - 1) {
        historyIndexRef.current = history.length;
        setCommand("");
      } else {
        historyIndexRef.current += 1;
        setCommand(history[historyIndexRef.current]);
      }
    }
  }

  const statusText = !running
    ? t("console.stopped")
    : stream === "live"
      ? t("console.live")
      : stream === "reconnecting"
        ? t("console.reconnecting")
        : t("console.connecting");

  return (
    <div className="flex h-full w-full flex-col gap-2">
      <div className="shrink-0 font-sans text-xs text-text-tertiary">{statusText}</div>
      <div ref={containerRef} className="min-h-0 grow border border-border bg-canvas p-2 [&_.xterm]:h-full" />
      <form
        className="flex shrink-0 items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          sendCommand();
        }}
      >
        <span className="text-primary-text" aria-hidden>
          &gt;
        </span>
        <Input
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!connected}
          placeholder={!running ? t("console.placeholderStopped") : connected ? t("console.placeholderReady") : t("console.placeholderConnecting")}
          className="grow font-mono"
          spellCheck={false}
          autoComplete="off"
        />
        <Button type="submit" disabled={!connected || !command.trim()}>
          {t("common.send")}
        </Button>
      </form>
    </div>
  );
}
