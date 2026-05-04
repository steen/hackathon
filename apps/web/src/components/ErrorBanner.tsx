import type * as React from "react";
import { useAuth } from "../auth/AuthContext.js";

export function ErrorBanner(): React.JSX.Element | null {
  const { error, dismissError } = useAuth();
  if (error === null) return null;
  return (
    <div
      className="error-banner"
      role="alert"
      aria-live="assertive"
      data-testid="auth-error-banner"
    >
      <span className="error-banner__msg">{error}</span>
      <button
        type="button"
        className="error-banner__dismiss"
        onClick={dismissError}
        aria-label="Dismiss error"
      >
        Dismiss
      </button>
    </div>
  );
}
