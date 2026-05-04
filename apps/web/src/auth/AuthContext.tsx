import type * as React from "react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { User } from "@hackathon/api-client";
import { getClient, readToken, writeToken } from "../api.js";

interface AuthState {
  token: string | null;
  user: User | null;
  loading: boolean;
  error: string | null;
}

interface AuthContextValue extends AuthState {
  login: (username: string, password: string) => Promise<void>;
  register: (username: string, password: string, inviteCode: string) => Promise<void>;
  logout: () => Promise<void>;
  dismissError: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message.length > 0 ? err.message : fallback;
}

export function AuthProvider({ children }: { children: ReactNode }): React.JSX.Element {
  const [state, setState] = useState<AuthState>(() => ({
    token: readToken(),
    user: null,
    loading: readToken() !== null,
    error: null,
  }));
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (state.token === null || state.user !== null) return;
    void (async () => {
      try {
        const u = await getClient().me();
        if (aliveRef.current) setState((s) => ({ ...s, user: u, loading: false }));
      } catch (err) {
        if (aliveRef.current) {
          writeToken(null);
          setState({
            token: null,
            user: null,
            loading: false,
            error: `Session could not be restored: ${errorMessage(err, "request failed")}`,
          });
        }
      }
    })();
  }, [state.token, state.user]);

  const login = useCallback(async (username: string, password: string) => {
    const out = await getClient().login(username, password);
    writeToken(out.token);
    setState({ token: out.token, user: out.user, loading: false, error: null });
  }, []);

  const register = useCallback(async (username: string, password: string, inviteCode: string) => {
    const out = await getClient().register(username, password, inviteCode);
    writeToken(out.token);
    setState({ token: out.token, user: out.user, loading: false, error: null });
  }, []);

  const logout = useCallback(async () => {
    let serverError: string | null = null;
    try {
      await getClient().logout();
    } catch (err) {
      serverError = `Signed out locally, but the server did not acknowledge: ${errorMessage(err, "request failed")}`;
    }
    writeToken(null);
    setState({ token: null, user: null, loading: false, error: serverError });
  }, []);

  const dismissError = useCallback(() => {
    setState((s) => (s.error === null ? s : { ...s, error: null }));
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ ...state, login, register, logout, dismissError }),
    [state, login, register, logout, dismissError],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const v = useContext(AuthContext);
  if (v === null) throw new Error("useAuth must be used within AuthProvider");
  return v;
}
