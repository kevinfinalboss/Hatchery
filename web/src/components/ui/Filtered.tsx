import { type ReactNode, useState } from "react";
import { Input } from "./Input";

export function Filtered<T>({
  items,
  text,
  placeholder,
  children,
}: {
  items: T[];
  text: (item: T) => string;
  placeholder: string;
  children: (shown: T[]) => ReactNode;
}) {
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const shown = needle ? items.filter((item) => text(item).toLowerCase().includes(needle)) : items;

  return (
    <div className="flex flex-col gap-3">
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="w-full max-w-xs"
      />
      {children(shown)}
    </div>
  );
}
