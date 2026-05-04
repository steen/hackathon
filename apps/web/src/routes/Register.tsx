import type * as React from "react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthContext.js";
import { formAuthMessage } from "../lib/userFacingError.js";

export function Register({ onSwitchToLogin }: { onSwitchToLogin: () => void }): React.JSX.Element {
  const { register } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteCode, setInviteCode] = useState("");
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
    setBusy(true);
    setError(null);
    try {
      await register(username, password, inviteCode);
    } catch (err) {
      setError(formAuthMessage("Registration failed", err));
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
