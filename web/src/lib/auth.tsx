import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, getAuthToken, setAuthToken } from "./api";
import { useI18n } from "./i18n";
import type { LoginResponse, TwoFactorChallenge, User } from "./types";

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<TwoFactorChallenge | null>;
  loginTwoFactor: (ticket: string, code: string) => Promise<LoginResponse>;
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

  async function login(username: string, password: string): Promise<TwoFactorChallenge | null> {
    const resp = await api.login(username, password);
    if ("twoFactorRequired" in resp) return resp;
    startSession(resp);
    return null;
  }

  async function loginTwoFactor(ticket: string, code: string) {
    const resp = await api.loginTwoFactor(ticket, code);
    startSession(resp);
    return resp;
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
    <AuthContext.Provider value={{ user, loading, login, loginTwoFactor, logout, setUser, startSession }}>{children}</AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}
