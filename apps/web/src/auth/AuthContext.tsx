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
import { ApiError, type User } from "@hackathon/api-client";
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

// Curated banner copy. The raw error never reaches the UI — only one of these
// strings does — so an internal-detail message can't leak into the banner.
const REASON_NETWORK = "Could not reach the server. Check your connection and try again.";
const REASON_SERVER_UNAVAILABLE = "The server is having trouble right now. Please try again.";
const REASON_SESSION_INVALID = "Your session is no longer valid. Please sign in again.";
const REASON_TIMEOUT = "The request timed out. Please try again.";
const REASON_CANCELED = "The request was canceled.";
const REASON_GENERIC = "Something went wrong.";

function classifyError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.status === 403) return REASON_SESSION_INVALID;
    if (err.status === 408) return REASON_TIMEOUT;
    if (err.status >= 500) return REASON_SERVER_UNAVAILABLE;
    // Other 4xx (400/404/409/422/429) collapse to REASON_GENERIC by design: this
    // helper runs in session-restore and logout paths where the user can't act on
    // a specific 4xx. Form-submission callers should map those statuses themselves.
    return REASON_GENERIC;
  }
  if (err instanceof Error) {
    if (err.name === "AbortError") return REASON_CANCELED;
    if (err.name === "TimeoutError") return REASON_TIMEOUT;
    // `fetch` rejects with a TypeError for DNS/refused/offline.
    if (err instanceof TypeError) return REASON_NETWORK;
  }
  return REASON_GENERIC;
}

function bannerMessage(prefix: string, err: unknown): string {
  // Keep the raw error available in devtools for diagnosis without surfacing it.
  console.error(prefix, err);
  return `${prefix}: ${classifyError(err)}`;
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
            error: bannerMessage("Session could not be restored", err),
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
      serverError = bannerMessage("Signed out locally, but the server did not acknowledge", err);
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
