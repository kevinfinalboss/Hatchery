import { useState } from "react";

export function useFragmentToken(): string {
  const [token] = useState(() => {
    const params = new URLSearchParams(window.location.hash.slice(1));
    const value = params.get("token") ?? "";
    if (value) window.history.replaceState(null, "", window.location.pathname + window.location.search);
    return value;
  });
  return token;
}
