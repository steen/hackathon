import type * as React from "react";
import { useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthContext.js";

export function Register({ onSwitchToLogin }: { onSwitchToLogin: () => void }): React.JSX.Element {
  const { register } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

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
      setError(err instanceof Error ? err.message : "registration failed");
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
