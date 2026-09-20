import { useCallback, useState } from "react";

export function usePersistedChoice<T extends string>(
  key: string,
  allowed: readonly T[],
  fallback: T,
): [T, (next: T) => void] {
  const [value, setValue] = useState<T>(() => {
    try {
      const stored = localStorage.getItem(key);
      if (stored && (allowed as readonly string[]).includes(stored)) return stored as T;
    } catch {
    }
    return fallback;
  });

  const set = useCallback(
    (next: T) => {
      setValue(next);
      try {
        localStorage.setItem(key, next);
      } catch {
      }
    },
    [key],
  );

  return [value, set];
}
