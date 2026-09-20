import { useEffect, useRef } from "react";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, basicSetup } from "codemirror";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { Button } from "../ui/Button";

function languageFor(filename: string): Extension[] {
  const ext = filename.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "json":
      return [json()];
    case "yml":
    case "yaml":
      return [yaml()];
    case "js":
    case "mjs":
    case "cjs":
      return [javascript()];
    default:
      return [];
  }
}

export function FileEditor({
  path,
  content,
  onSave,
  onClose,
  saving,
}: {
  path: string;
  content: string;
  onSave: (content: string) => void;
  onClose: () => void;
  saving: boolean;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const view = new EditorView({
      state: EditorState.create({
        doc: content,
        extensions: [
          basicSetup,
          ...languageFor(path),
          EditorView.theme({
            "&": { height: "100%", fontSize: "13px" },
            ".cm-scroller": { fontFamily: "'JetBrains Mono', monospace" },
          }),
        ],
      }),
      parent: container,
    });
    viewRef.current = view;

    return () => view.destroy();
    // Intentionally mount-only: the parent remounts this component with a
    // new `key={path}` (see FileManager.tsx) whenever a different file is
    // opened, instead of this effect reacting to prop changes — reacting
    // here would reset the cursor/undo history on every re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="flex h-full w-full flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="font-mono text-sm text-text-secondary">{path}</span>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={onClose}>
            Fechar
          </Button>
          <Button
            disabled={saving}
            onClick={() => {
              if (viewRef.current) onSave(viewRef.current.state.doc.toString());
            }}
          >
            Salvar
          </Button>
        </div>
      </div>
      <div ref={containerRef} className="min-h-0 grow overflow-hidden rounded-lg border border-border" />
    </div>
  );
}
