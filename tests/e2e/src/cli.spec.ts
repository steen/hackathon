import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { startFixture, type ServerFixture } from "./serverFixture.js";
import { newCliSession } from "./cliRunner.js";
import {
  createChannel,
  loginUser,
  postMessage,
  listPresence,
  debugSubsCount,
  uniqueUsername,
} from "./restClient.js";

const TEST_PASSWORD = "e2e-fake-pw-1234567890";

// Each scenario gets its own server fixture so per-IP rate limits
// (RegisterIPConfig: 5 attempts / 15 min) do not bleed across tests.
// The Go binaries are cached after the first build; the per-test cost
// is mostly process spawn + a short readiness probe (~200-400ms).
describe("CLI e2e (real chatd against real server)", () => {
  let fx!: ServerFixture;

  beforeEach(async () => {
    fx = await startFixture();
  });

  afterEach(async () => {
    try {
      await fx.cleanup();
    } catch (e) {
      console.error("[e2e] fixture cleanup error:", e);
    }
  });

  it("AC: register → login → list channels → create channel → post message → list contains it", async () => {
    const cli = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const username = uniqueUsername("u-cli-flow");

      // register via CLI (writes token to isolated XDG_CONFIG_HOME)
      const reg = await cli.run(["register", username], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      expect(reg.exitCode, `register stderr: ${reg.stderr}`).toBe(0);
      expect(reg.stdout).toContain(`Registered as ${username}`);

      // login again to confirm the same path works after register
      const login = await cli.run(["login", "--username", username], {
        env: { CHAT_PASSWORD: TEST_PASSWORD },
      });
      expect(login.exitCode, `login stderr: ${login.stderr}`).toBe(0);
      expect(login.stdout).toContain(`Logged in as ${username}`);

      // list channels (fresh DB has the phase-3-seeded #general row)
      const chBefore = await cli.run(["channels"]);
      expect(chBefore.exitCode).toBe(0);
      const beforeLines = chBefore.stdout.trim().split("\n").filter(Boolean);
      expect(beforeLines.length).toBe(1);
      expect(beforeLines[0]).toMatch(/\tgeneral$/);

      // create channel via REST (CLI surface has no create-channel; same as smoke.sh)
      const tok = (await loginUser(fx.baseUrl, username, TEST_PASSWORD)).token;
      const channelName = uniqueUsername("ch");
      const channel = await createChannel(fx.baseUrl, tok, channelName);
      expect(channel.id).toBeTruthy();

      // CLI sees the new channel
      const chAfter = await cli.run(["channels"]);
      expect(chAfter.exitCode).toBe(0);
      expect(chAfter.stdout).toContain(channel.id);
      expect(chAfter.stdout).toContain(channelName);

      // CLI posts a message
      const body = `hello-from-cli-${String(Date.now())}`;
      const send = await cli.run(["send", channel.id, body]);
      expect(send.exitCode, `send stderr: ${send.stderr}`).toBe(0);
      expect(send.stdout.trim().length).toBeGreaterThan(0);

      // CLI history includes the body
      const hist = await cli.run(["history", channel.id]);
      expect(hist.exitCode).toBe(0);
      expect(hist.stdout).toContain(body);
    } finally {
      cli.cleanup();
    }
  });

  it("AC: chatd watch receives a message posted by a second client over REST", async () => {
    const watcher = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const username = uniqueUsername("u-cli-watch");
      const reg = await watcher.run(["register", username], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      expect(reg.exitCode).toBe(0);

      const tok = (await loginUser(fx.baseUrl, username, TEST_PASSWORD)).token;
      const channel = await createChannel(fx.baseUrl, tok, uniqueUsername("ch"));

      const long = watcher.spawnLong(["watch", channel.id]);
      try {
        // Wait for the watcher's WS to register with the hub via /debug/subs.
        await waitFor(async () => (await debugSubsCount(fx.baseUrl, channel.id)) >= 1, 5000);

        const body = `from-rest-${String(Date.now())}`;
        await postMessage(fx.baseUrl, tok, channel.id, body);

        await waitFor(() => Promise.resolve(long.stdout().includes(body)), 5000);
        expect(long.stdout()).toContain(body);
      } finally {
        await long.stop();
      }
    } finally {
      watcher.cleanup();
    }
  });

  it("AC: logout invalidates subsequent CLI calls", async () => {
    const cli = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const username = uniqueUsername("u-cli-logout");
      await cli.run(["register", username], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });

      const before = await cli.run(["whoami"]);
      expect(before.exitCode).toBe(0);
      expect(before.stdout.trim()).toBe(username);

      const out = await cli.run(["logout"]);
      expect(out.exitCode).toBe(0);

      const after = await cli.run(["whoami"]);
      // CLI returns non-zero when the local token is missing or the
      // server rejects it. Either way we want the call to fail.
      expect(after.exitCode).not.toBe(0);
    } finally {
      cli.cleanup();
    }
  });

  it("AC: starting chatd watch shows up in /api/presence; closing removes it", async () => {
    const watcher = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    const observer = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const u = uniqueUsername("u-cli-presence");
      await watcher.run(["register", u], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      // Need a separate user just to read /api/presence (auth-required).
      const obs = uniqueUsername("u-cli-presence-obs");
      await observer.run(["register", obs], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      const obsTok = (await loginUser(fx.baseUrl, obs, TEST_PASSWORD)).token;
      const watcherInfo = await loginUser(fx.baseUrl, u, TEST_PASSWORD);
      const channel = await createChannel(fx.baseUrl, watcherInfo.token, uniqueUsername("ch"));

      const long = watcher.spawnLong(["watch", channel.id]);
      try {
        await waitFor(async () => {
          const p = await listPresence(fx.baseUrl, obsTok);
          return p.users.some((x) => x.id === watcherInfo.user.id);
        }, 5000);
      } finally {
        await long.stop();
      }
      // After stop, the watcher's WS subscription closes; presence should drop.
      await waitFor(async () => {
        const p = await listPresence(fx.baseUrl, obsTok);
        return !p.users.some((x) => x.id === watcherInfo.user.id);
      }, 5000);
    } finally {
      watcher.cleanup();
      observer.cleanup();
    }
  });

  it("AC: reconnect on server restart — chatd watch resumes without manual restart", async () => {
    const watcher = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const u = uniqueUsername("u-cli-reconn");
      await watcher.run(["register", u], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      const tok = (await loginUser(fx.baseUrl, u, TEST_PASSWORD)).token;
      const channel = await createChannel(fx.baseUrl, tok, uniqueUsername("ch"));

      const long = watcher.spawnLong(["watch", channel.id]);
      try {
        // First message lands while the original server is alive.
        await waitFor(async () => (await debugSubsCount(fx.baseUrl, channel.id)) >= 1, 5000);
        const body1 = `pre-restart-${String(Date.now())}`;
        await postMessage(fx.baseUrl, tok, channel.id, body1);
        await waitFor(() => Promise.resolve(long.stdout().includes(body1)), 5000);

        // Restart the server (same port, same DB so the channel + token survive).
        await fx.restart();

        // Watcher re-attaches via its own backoff. Need to refresh the token —
        // /api/auth/login returns a new bearer; the watch loop reuses the
        // stored one which is still valid because JWT_SECRET is unchanged.
        await waitFor(async () => (await debugSubsCount(fx.baseUrl, channel.id)) >= 1, 15_000);

        const body2 = `post-restart-${String(Date.now())}`;
        await postMessage(fx.baseUrl, tok, channel.id, body2);
        await waitFor(() => Promise.resolve(long.stdout().includes(body2)), 10_000);
        expect(long.stdout()).toContain(body2);
      } finally {
        await long.stop();
      }
    } finally {
      watcher.cleanup();
    }
  }, 60_000);
});

async function waitFor(check: () => Promise<boolean>, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      if (await check()) return;
    } catch (e) {
      lastErr = e;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  const tail = lastErr === null ? "" : `; last error: ${formatErr(lastErr)}`;
  throw new Error(`condition not met within ${String(timeoutMs)}ms${tail}`);
}

function formatErr(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  try {
    return JSON.stringify(e);
  } catch {
    return "<unstringifiable>";
  }
}
