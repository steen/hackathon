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
import { b64, deriveIdentity, ready as sodiumReady, type User } from "@hackathon/api-client";
import { getClient, readToken, writeToken } from "../api.js";
import { writeIdentitySeed, clearIdentitySeed } from "../lib/identityStore.js";
import { bannerMessage } from "../lib/userFacingError.js";

interface AuthState {
  token: string | null;
  user: User | null;
  loading: boolean;
  error: string | null;
}

// WrongPassphraseError is the typed signal Login.tsx checks for so the
// "wrong identity passphrase" branch surfaces a specific message rather
// than the generic 401 fallback. Decision-log §4 + L4: wrong-passphrase
// detection happens client-side by comparing the locally-derived
// sign_pubkey against the server-stored one before any sensitive
// operation runs.
export class WrongPassphraseError extends Error {
  constructor(message = "wrong identity passphrase") {
    super(message);
    this.name = "WrongPassphraseError";
  }
}

interface AuthContextValue extends AuthState {
  login: (username: string, password: string, identityPassphrase?: string) => Promise<void>;
  register: (
    username: string,
    password: string,
    inviteCode: string,
    identityPassphrase?: string,
  ) => Promise<void>;
  logout: () => Promise<void>;
  dismissError: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

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

  const login = useCallback(
    async (username: string, password: string, identityPassphrase?: string) => {
      const out = await getClient().login(username, password);
      if (identityPassphrase !== undefined && identityPassphrase.length > 0) {
        await sodiumReady();
        const id = await deriveIdentity(identityPassphrase, username);
        const localSignPub = b64(id.signPub);
        const remoteSignPub = out.user.sign_pubkey ?? "";
        if (remoteSignPub !== "" && remoteSignPub !== localSignPub) {
          // Server-stored sign_pubkey is the canary (decision-log §4 +
          // L4). Refuse to keep the freshly-issued token when the
          // derived identity does not match — the user typed the wrong
          // identity passphrase.
          await getClient()
            .logout()
            .catch(() => undefined);
          writeToken(null);
          throw new WrongPassphraseError();
        }
        await writeIdentitySeed(out.user.id, id.rootSeed);
      }
      writeToken(out.token);
      setState({ token: out.token, user: out.user, loading: false, error: null });
    },
    [],
  );

  const register = useCallback(
    async (username: string, password: string, inviteCode: string, identityPassphrase?: string) => {
      let identity: { boxPubkey: string; signPubkey: string } | undefined;
      let rootSeed: Uint8Array | undefined;
      if (identityPassphrase !== undefined && identityPassphrase.length > 0) {
        await sodiumReady();
        const id = await deriveIdentity(identityPassphrase, username);
        identity = { boxPubkey: b64(id.boxPub), signPubkey: b64(id.signPub) };
        rootSeed = id.rootSeed;
      }
      const out = await getClient().register(username, password, inviteCode, identity);
      if (rootSeed) {
        await writeIdentitySeed(out.user.id, rootSeed);
      }
      writeToken(out.token);
      setState({ token: out.token, user: out.user, loading: false, error: null });
    },
    [],
  );

  const logout = useCallback(async () => {
    let serverError: string | null = null;
    try {
      await getClient().logout();
    } catch (err) {
      serverError = bannerMessage("Signed out locally, but the server did not acknowledge", err);
    }
    writeToken(null);
    await clearIdentitySeed().catch(() => undefined);
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
