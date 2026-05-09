import type * as React from "react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useAuth, WrongPassphraseError } from "../auth/AuthContext.js";
import { formAuthMessage } from "../lib/userFacingError.js";

export function Login({
  onSwitchToRegister,
}: {
  onSwitchToRegister: () => void;
}): React.JSX.Element {
  const { login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [identityPassphrase, setIdentityPassphrase] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const usernameRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    usernameRef.current?.focus();
  }, []);

  async function onSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(username, password, identityPassphrase || undefined);
    } catch (err) {
      if (err instanceof WrongPassphraseError) {
        setError("Wrong identity passphrase. Please try again.");
      } else {
        setError(formAuthMessage("Login failed", err));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="auth-page">
      <h1>Sign in</h1>
      <form
        onSubmit={(e) => {
          void onSubmit(e);
        }}
      >
        <label>
          <span>Username</span>
          <input
            ref={usernameRef}
            name="username"
            value={username}
            onChange={(e) => {
              setUsername(e.target.value);
            }}
            autoComplete="username"
            required
          />
        </label>
        <label>
          <span>Password</span>
          <input
            name="password"
            type="password"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
            }}
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          <span>Identity passphrase</span>
          <input
            name="identity-passphrase"
            type="password"
            value={identityPassphrase}
            onChange={(e) => {
              setIdentityPassphrase(e.target.value);
            }}
            autoComplete="current-password"
          />
        </label>
        {error !== null ? (
          <p role="alert" className="error">
            {error}
          </p>
        ) : null}
        <button type="submit" disabled={busy}>
          {busy ? "Signing in..." : "Sign in"}
        </button>
      </form>
      <p>
        No account?{" "}
        <button type="button" onClick={onSwitchToRegister} className="linklike">
          Register
        </button>
      </p>
    </main>
  );
}
