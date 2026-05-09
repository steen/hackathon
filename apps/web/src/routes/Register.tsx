import type * as React from "react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { MIN_IDENTITY_PASSPHRASE_LEN } from "@hackathon/api-client";
import { useAuth } from "../auth/AuthContext.js";
import { registerAuthMessage } from "../lib/userFacingError.js";

export function Register({ onSwitchToLogin }: { onSwitchToLogin: () => void }): React.JSX.Element {
  const { register } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [identityPassphrase, setIdentityPassphrase] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const usernameRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    usernameRef.current?.focus();
  }, []);

  async function onSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    if (inviteCode.trim().length === 0) {
      setError("invite code is required");
      return;
    }
    if (identityPassphrase.length > 0 && identityPassphrase.length < MIN_IDENTITY_PASSPHRASE_LEN) {
      setError(
        `identity passphrase must be at least ${String(MIN_IDENTITY_PASSPHRASE_LEN)} characters`,
      );
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await register(username, password, inviteCode, identityPassphrase || undefined);
    } catch (err) {
      setError(registerAuthMessage("Registration failed", err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="auth-page">
      <h1>Register</h1>
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
            autoComplete="new-password"
            required
          />
        </label>
        <label>
          <span>Invite code</span>
          <input
            name="invite_code"
            value={inviteCode}
            onChange={(e) => {
              setInviteCode(e.target.value);
            }}
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
            autoComplete="new-password"
            minLength={MIN_IDENTITY_PASSPHRASE_LEN}
          />
        </label>
        {error !== null ? (
          <p role="alert" className="error">
            {error}
          </p>
        ) : null}
        <button type="submit" disabled={busy}>
          {busy ? "Creating account..." : "Create account"}
        </button>
      </form>
      <p>
        Already have an account?{" "}
        <button type="button" onClick={onSwitchToLogin} className="linklike">
          Sign in
        </button>
      </p>
    </main>
  );
}
