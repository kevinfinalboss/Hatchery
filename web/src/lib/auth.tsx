import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, getAuthToken, setAuthToken } from "./api";
import { useI18n } from "./i18n";
import type { LoginResponse, User } from "./types";

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  setUser: (user: User) => void;
  startSession: (resp: LoginResponse) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUserState] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const { setLocale, setTimeZone } = useI18n();

  // The profile's language and time zone follow the account across devices.
  const setUser = useCallback(
    (u: User) => {
      setUserState(u);
      if (u.locale) setLocale(u.locale);
      setTimeZone(u.timeZone);
    },
    [setLocale, setTimeZone],
  );

  useEffect(() => {
    if (!getAuthToken()) {
      setLoading(false);
      return;
    }
    api
      .me()
      .then(setUser)
      .catch(() => setAuthToken(null))
      .finally(() => setLoading(false));
  }, [setUser]);

  function startSession(resp: LoginResponse) {
    setAuthToken(resp.token);
    setUser(resp.user);
  }

  async function login(username: string, password: string) {
    startSession(await api.login(username, password));
  }

  async function logout() {
    try {
      await api.logout();
    } catch {
    }
    setAuthToken(null);
    setUserState(null);
    setTimeZone("");
  }

  return (
    <AuthContext.Provider value={{ user, loading, login, logout, setUser, startSession }}>{children}</AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}
