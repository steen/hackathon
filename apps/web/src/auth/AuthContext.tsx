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
}

interface AuthContextValue extends AuthState {
  login: (username: string, password: string) => Promise<void>;
  register: (username: string, password: string, inviteCode: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }): React.JSX.Element {
  const [state, setState] = useState<AuthState>(() => ({
    token: readToken(),
    user: null,
    loading: readToken() !== null,
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
      } catch {
        if (aliveRef.current) {
          writeToken(null);
          setState({ token: null, user: null, loading: false });
        }
      }
    })();
  }, [state.token, state.user]);

  const login = useCallback(async (username: string, password: string) => {
    const out = await getClient().login(username, password);
    writeToken(out.token);
    setState({ token: out.token, user: out.user, loading: false });
  }, []);

  const register = useCallback(async (username: string, password: string, inviteCode: string) => {
    const out = await getClient().register(username, password, inviteCode);
    writeToken(out.token);
    setState({ token: out.token, user: out.user, loading: false });
  }, []);

  const logout = useCallback(async () => {
    try {
      await getClient().logout();
    } catch {
      /* network error on logout still clears local state */
    }
    writeToken(null);
    setState({ token: null, user: null, loading: false });
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ ...state, login, register, logout }),
    [state, login, register, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const v = useContext(AuthContext);
  if (v === null) throw new Error("useAuth must be used within AuthProvider");
  return v;
}
