import { useEffect, useState } from "react";
import { ApiError } from "@hackathon/api-client";

// Curated user-facing copy. The raw error never reaches the UI — only one of
// these strings does — so an internal-detail message (stack-trace flavored
// fetch text, server error envelope, jwt diagnostic, etc.) cannot leak into
// a banner or inline form error.
//
// Keep this set small and overlapping causes deliberately collapsed:
//   - ApiError 401/403 in a session-restore / hooks context maps to
//     SESSION_INVALID; the same status from a Login form-submit context maps
//     to INVALID_CREDENTIALS via formAuthMessage(); the same status from a
//     Register form-submit context maps to INVITE_REJECTED via
//     registerAuthMessage() — Register has no password to mismatch, so the
//     realistic 401 cause is an invite-code rejection.
//   - 400/422 from a form maps to VALIDATION via formAuthMessage() /
//     registerAuthMessage(); from a hooks context they fall through to
//     GENERIC because the hook can't say which field is wrong.
//
// Add a new REASON_* only when a real call site has an outcome the existing
// set genuinely can't describe — not as a stylistic preference.

export const REASON_NETWORK = "Could not reach the server. Check your connection and try again.";
export const REASON_SERVER_UNAVAILABLE =
  "The server is having trouble right now. Please try again.";
export const REASON_SESSION_INVALID = "Your session is no longer valid. Please sign in again.";
export const REASON_TIMEOUT = "The request timed out. Please try again.";
export const REASON_CANCELED = "The request was canceled.";
export const REASON_GENERIC = "Something went wrong.";
export const REASON_INVALID_CREDENTIALS =
  "That username and password don't match. Please try again.";
export const REASON_INVITE_REJECTED =
  "That invite code wasn't accepted. Please check it and try again.";
export const REASON_VALIDATION = "Please check the form and try again.";

export function classifyError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.status === 403) return REASON_SESSION_INVALID;
    if (err.status === 408) return REASON_TIMEOUT;
    if (err.status >= 500) return REASON_SERVER_UNAVAILABLE;
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

// Form-submit variant: 401 means "wrong credentials" (not "your session
// expired"), and 400/422 means "the server rejected the inputs." Other
// statuses share the closed set with classifyError().
export function classifyFormAuthError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.status === 403) return REASON_INVALID_CREDENTIALS;
    if (err.status === 400 || err.status === 422) return REASON_VALIDATION;
  }
  return classifyError(err);
}

export function bannerMessage(prefix: string, err: unknown): string {
  // Keep the raw error visible in devtools for diagnosis without surfacing it.
  console.error(prefix, err);
  return `${prefix}: ${classifyError(err)}`;
}

// Same console-tap as bannerMessage(), but returns just the curated reason
// (no prefix) for callers that already render the prefix elsewhere — e.g.
// the channel list and message hooks, where the banner copy is structured
// "<heading>: <reason>" by the surrounding component, or for a form
// submit-error <p role="alert"> that shows only the reason.
export function userFacingMessage(prefix: string, err: unknown): string {
  console.error(prefix, err);
  return classifyError(err);
}

export function formAuthMessage(prefix: string, err: unknown): string {
  console.error(prefix, err);
  return classifyFormAuthError(err);
}

// Register-form variant: 401/403 means the invite code was rejected (Register
// has no password to mismatch, so REASON_INVALID_CREDENTIALS is the wrong
// copy). 400/422 still means "the server rejected the inputs." Other statuses
// share the closed set with classifyError().
export function classifyRegisterAuthError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.status === 403) return REASON_INVITE_REJECTED;
    if (err.status === 400 || err.status === 422) return REASON_VALIDATION;
  }
  return classifyError(err);
}

export function registerAuthMessage(prefix: string, err: unknown): string {
  console.error(prefix, err);
  return classifyRegisterAuthError(err);
}

// App-error sink: a tiny module-level pub/sub used to surface hook-level
// failures (presence seed, channels list, message history/socket) via the
// App-shell ErrorBanner. The AuthContext keeps its own `error` field for
// session-related faults; this sink covers everything else and is read by
// ErrorBanner alongside the auth error so users see one global notice rather
// than per-feature inline state. Hooks call `reportAppError()` once per
// failure (inside their catch blocks); the banner calls `dismissAppError()`
// on user dismiss. Single-slot semantics — only the latest reported message
// is held — match the existing ErrorBanner's single-message UX.
//
// Module-level so the publishing call site (a hook's catch handler, possibly
// running outside of React's tree on a delayed promise) doesn't have to find
// a context value via `useContext`. Subscriptions go through React via
// `useAppError()` so the banner re-renders on dispatch.

type AppErrorListener = (msg: string | null) => void;

let currentAppError: string | null = null;
const appErrorListeners = new Set<AppErrorListener>();

export function reportAppError(msg: string): void {
  if (msg === currentAppError) return;
  currentAppError = msg;
  for (const fn of appErrorListeners) fn(currentAppError);
}

export function dismissAppError(): void {
  if (currentAppError === null) return;
  currentAppError = null;
  for (const fn of appErrorListeners) fn(null);
}

// Test helper — also useful between component test cases. Not exported in
// production paths beyond test hooks resetting suite-shared state.
export function _resetAppErrorSinkForTests(): void {
  currentAppError = null;
  appErrorListeners.clear();
}

function subscribeAppError(fn: AppErrorListener): () => void {
  appErrorListeners.add(fn);
  return () => {
    appErrorListeners.delete(fn);
  };
}

export function useAppError(): string | null {
  const [err, setErr] = useState<string | null>(currentAppError);
  useEffect(() => {
    setErr(currentAppError);
    return subscribeAppError(setErr);
  }, []);
  return err;
}
