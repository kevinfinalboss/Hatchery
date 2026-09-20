import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { api } from "../../lib/api";
import { Input } from "../ui/Input";
import { Button } from "../ui/Button";

export function ServerConsole({ org, name }: { org: string; name: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const historyRef = useRef<string[]>([]);
  const historyIndexRef = useRef(0);

  const [connected, setConnected] = useState(false);
  const [command, setCommand] = useState("");

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = new Terminal({
      convertEol: true,
      disableStdin: true,
      fontFamily: "'JetBrains Mono', monospace",
      fontSize: 13,
      theme: {
        background: "#08111f",
        foreground: "#f3f6fc",
        cursor: "#3f80ec",
        selectionBackground: "#29406e",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(container);
    fit.fit();
    termRef.current = term;

    let cancelled = false;
    let ws: WebSocket | null = null;

    void (async () => {
      try {
        const { ticket } = await api.consoleTicket(org, name);
        if (cancelled) return;

        ws = new WebSocket(api.consoleUrl(org, name, ticket));
        wsRef.current = ws;

        ws.onopen = () => {
          setConnected(true);
          term.write("\x1b[90m-- conectado --\x1b[0m\r\n");
        };
        ws.onmessage = (event) => {
          if (typeof event.data === "string") {
            term.write(event.data);
          } else {
            void (event.data as Blob).arrayBuffer().then((buf) => term.write(new Uint8Array(buf)));
          }
        };
        ws.onclose = () => {
          setConnected(false);
          term.write("\r\n\x1b[90m-- desconectado --\x1b[0m\r\n");
        };
        ws.onerror = () => term.write("\r\n\x1b[31m-- erro de conexão --\x1b[0m\r\n");
      } catch (err) {
        const msg = err instanceof Error ? err.message : "falha ao abrir o console";
        term.write(`\r\n\x1b[31m-- ${msg} --\x1b[0m\r\n`);
      }
    })();

    const resizeObserver = new ResizeObserver(() => fit.fit());
    resizeObserver.observe(container);

    return () => {
      cancelled = true;
      resizeObserver.disconnect();
      ws?.close();
      term.dispose();
      termRef.current = null;
      wsRef.current = null;
    };
  }, [org, name]);

  function sendCommand() {
    const trimmed = command.trim();
    const ws = wsRef.current;
    const term = termRef.current;
    if (!trimmed || !term || !ws || ws.readyState !== WebSocket.OPEN) return;

    term.write(`\x1b[36m> ${trimmed}\x1b[0m\r\n`);
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

  return (
    <div className="flex h-full w-full flex-col gap-2">
      <div ref={containerRef} className="min-h-0 grow [&_.xterm]:h-full" />
      <form
        className="flex shrink-0 items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          sendCommand();
        }}
      >
        <Input
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!connected}
          placeholder={connected ? "Digite um comando e pressione Enter" : "Conectando..."}
          className="grow font-mono"
          spellCheck={false}
          autoComplete="off"
        />
        <Button type="submit" disabled={!connected || !command.trim()}>
          Enviar
        </Button>
      </form>
    </div>
  );
}
