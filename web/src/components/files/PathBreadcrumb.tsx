export function PathBreadcrumb({ path, file, onNavigate }: { path: string; file?: boolean; onNavigate: (dir: string) => void }) {
  const segments = path === "/" ? [] : path.split("/").filter(Boolean);
  const dirs = file ? segments.slice(0, -1) : segments;
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1 font-mono text-sm text-text-secondary">
      <button type="button" className="hover:text-text-primary" onClick={() => onNavigate("/")}>
        /
      </button>
      {dirs.map((segment, i) => (
        <span key={i} className="flex items-center gap-1">
          <button type="button" className="hover:text-text-primary" onClick={() => onNavigate("/" + dirs.slice(0, i + 1).join("/"))}>
            {segment}
          </button>
          {(i < dirs.length - 1 || file) && <span>/</span>}
        </span>
      ))}
      {file && <span className="text-text-primary">{segments[segments.length - 1]}</span>}
    </div>
  );
}
