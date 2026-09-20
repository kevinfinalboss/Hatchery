import { useEffect, useRef, useState } from "react";
import { api, getAuthToken } from "../../lib/api";

function colorFor(line: string): string {
  if (/error|fatal/i.test(line)) return "text-status-failed";
  if (/warn/i.test(line)) return "text-status-installing";
  return "text-text-secondary";
}

export function ServerLogs({ org, name }: { org: string; name: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLines([]);

    async function stream() {
      const token = getAuthToken();
      const res = await fetch(api.logsPath(org, name), {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        signal: controller.signal,
      });
      if (!res.body) return;
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split("\n");
        buffer = parts.pop() ?? "";
        if (parts.length > 0) {
          setLines((prev) => [...prev, ...parts]);
        }
      }
    }

    stream().catch(() => {
    });
    return () => controller.abort();
  }, [org, name]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [lines]);

  return (
    <div className="h-full overflow-y-auto rounded-lg border border-border bg-sidebar p-3 font-mono text-xs leading-relaxed">
      {lines.length === 0 && <div className="text-text-tertiary">Aguardando logs…</div>}
      {lines.map((line, i) => (
        <div key={i} className={colorFor(line)}>
          {line || " "}
        </div>
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
