import type * as React from "react";
import { useAuth } from "../auth/AuthContext.js";
import { dismissAppError, useAppError } from "../lib/userFacingError.js";

// Surfaces app-level failures from two sources:
//   1. AuthContext.error — session-restore and logout faults.
//   2. The shared app-error sink in lib/userFacingError.ts — populated by
//      the presence/channels/messages hooks on REST or WS faults.
//
// Auth wins when both are set: a session-invalid notice is more actionable
// than a generic "couldn't load channels" once the user is already being
// asked to sign in again. Dismiss clears whichever source is currently
// showing so the next failure (or unrelated source) can surface.
export function ErrorBanner(): React.JSX.Element | null {
  const { error: authError, dismissError: dismissAuthError } = useAuth();
  const sinkError = useAppError();
  const showing = authError ?? sinkError;
  if (showing === null) return null;
  const isAuth = authError !== null;
  function onDismiss(): void {
    if (isAuth) {
      dismissAuthError();
    } else {
      dismissAppError();
    }
  }
  return (
    <div
      className="error-banner"
      role="alert"
      aria-live="assertive"
      data-testid="auth-error-banner"
    >
      <span className="error-banner__msg">{showing}</span>
      <button
        type="button"
        className="error-banner__dismiss"
        onClick={onDismiss}
        aria-label="Dismiss error"
      >
        Dismiss
      </button>
    </div>
  );
}
