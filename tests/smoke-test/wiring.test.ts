import { existsSync, readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";

// Static-source assertions for specs/plans/phase-0/feature-smoke-test.md.
// The script's runtime behavior (boot server, broadcast, teardown) is
// verified by actually running it — `pnpm test` invokes
// `bash scripts/smoke.sh` before this vitest workspace runs. These tests
// guard against silent regressions in the script's structure (someone
// removes the trap, drops `set -euo pipefail`, etc.) without needing to
// re-run the full smoke flow.

const repoRoot = resolve(__dirname, "..", "..");
const smokePath = resolve(repoRoot, "scripts", "smoke.sh");
const pkgPath = resolve(repoRoot, "package.json");

function smokeBody(): string {
  return readFileSync(smokePath, "utf8");
}

describe("smoke-test: wiring + script structure", () => {
  it("TestAC1_smoke_test_script_executes_the_documented_flow", () => {
    const body = smokeBody();
    // Server build + binary invocation
    expect(/go build .*\.\/apps\/server/.test(body), "must build apps/server").toBe(true);
    expect(/go build .*\.\/apps\/cli/.test(body), "must build apps/cli (chatd)").toBe(true);
    // Two `chatd watch` processes
    const watchInvocations = body.match(/\bwatch\b/g) ?? [];
    expect(
      watchInvocations.length,
      `expected at least 2 'watch' invocations in script, got ${String(watchInvocations.length)}`,
    ).toBeGreaterThanOrEqual(2);
    // Audit #78 dropped the legacy raw rebroadcast on /ws — the smoke
    // script now produces messages via REST POST /api/channels/{id}/messages
    // instead of `chatd send` (which wrote a raw WS frame the server
    // would now drop silently).
    expect(
      /api\/channels\/.+\/messages/.test(body),
      "must POST to /api/channels/{id}/messages (REST producer path)",
    ).toBe(true);
    // Watcher-output assertion: greps the watcher output files for the message
    expect(/grep -F.*\$MSG/.test(body), "must grep watcher output files for the sent message").toBe(
      true,
    );
  });

  it("TestAC1_smoke_test_uses_debug_subs_for_deterministic_subscriber_readiness", () => {
    // The script must NOT use a fixed sleep to wait for both watchers to
    // register with the hub — that race was the original flake. Per the spec
    // risks section it polls /debug/subs?channel=<id> until the count
    // reaches 2 (5s budget). Catches a regression where someone reverts to
    // a sleep-based wait. The channel value is now the per-run ULID (audit
    // #78 follow-up: REST producer path requires a real channel row), not
    // the legacy #general sentinel.
    const body = smokeBody();
    expect(body.includes("/debug/subs"), "must poll /debug/subs to wait for subscribers").toBe(
      true,
    );
    expect(
      body.includes("/debug/subs?channel=${CHANNEL_ID}") ||
        body.includes("channel=%23general"),
      "must poll /debug/subs with the smoke channel id (or legacy #general)",
    ).toBe(true);
    expect(
      /EXPECTED_SUBS=\s*2\b/.test(body),
      "must wait for exactly 2 subscribers (one per watcher)",
    ).toBe(true);
  });

  it("TestAC2_smoke_test_script_uses_strict_mode_and_explicit_failure_output", () => {
    const body = smokeBody();
    expect(
      /^set -euo pipefail$/m.test(body),
      "must use 'set -euo pipefail' for non-zero on first error",
    ).toBe(true);
    // The failure path must produce a clear stderr message — not just `exit 1`.
    expect(
      /FAIL[^\n]*>&2/.test(body) || /echo[^\n]*FAIL[^\n]*>&2/.test(body),
      "must print a FAIL line to stderr on failure",
    ).toBe(true);
  });

  it("TestAC3_smoke_test_script_traps_exit_and_kills_spawned_pids", () => {
    const body = smokeBody();
    // The trap must cover normal exit AND signal-driven exit (interrupted CI).
    expect(/trap\s+\w+\s+EXIT/.test(body), "must trap EXIT").toBe(true);
    expect(/trap\s+\w+\s+[A-Z ]*INT/.test(body), "must trap INT (so Ctrl-C cleans up)").toBe(true);
    // The cleanup function must kill the recorded PIDs.
    expect(/SERVER_PID|WATCH1_PID|WATCH2_PID/.test(body), "must record PIDs to clean up").toBe(
      true,
    );
    expect(/\bkill\b/.test(body), "cleanup must invoke kill on the recorded PIDs").toBe(true);
  });

  it("TestAC3_smoke_test_cleanup_escalates_term_to_kill_for_wedged_children", () => {
    // Per spec risks: wedged children that ignore SIGTERM must not hang
    // `wait` indefinitely. Cleanup escalates SIGTERM -> SIGKILL after a
    // bounded wait. Catches a regression where someone simplifies cleanup
    // back to a single `kill $pid; wait` (which deadlocks on a wedged child).
    const body = smokeBody();
    expect(
      /kill\s+-KILL\s+"\$pid"/.test(body) || /kill\s+-9\s+"\$pid"/.test(body),
      "cleanup must escalate to SIGKILL for children that ignore SIGTERM",
    ).toBe(true);
    // The escalation must be guarded by a kill -0 liveness check so we don't
    // -KILL an already-exited PID (which would race with `wait`).
    expect(
      /kill\s+-0\s+"\$pid"/.test(body),
      "cleanup must probe with kill -0 before escalating to SIGKILL",
    ).toBe(true);
  });

  it("TestAC4_smoke_test_script_is_wired_into_root_package_json_test_script", () => {
    expect(existsSync(smokePath), "scripts/smoke.sh must exist").toBe(true);
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8")) as { scripts?: Record<string, string> };
    const testBody: string = pkg.scripts?.test ?? "";
    const directlyInvokes = /smoke\.sh\b/.test(testBody);
    const fansOut = /pnpm\s+(?:-r|--recursive)\b/.test(testBody);
    expect(
      directlyInvokes || fansOut,
      `package.json scripts.test must invoke smoke.sh directly or fan out to a workspace that does; got: ${testBody}`,
    ).toBe(true);
  });

  it("TestAC5_smoke_test_script_is_executable_and_present", () => {
    expect(existsSync(smokePath), "scripts/smoke.sh must exist").toBe(true);
    const mode = statSync(smokePath).mode & 0o111;
    expect(
      mode,
      "scripts/smoke.sh must have at least one execute bit set so 'bash scripts/smoke.sh' (or direct ./scripts/smoke.sh) works in CI",
    ).not.toBe(0);
  });
});
