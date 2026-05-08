// Boots the real `apps/server` binary against a per-fixture tempdir + SQLite +
// ephemeral CHAT_INVITE_CODE / CHAT_JWT_SECRET. Used by every CLI / interop /
// Playwright scenario so failures tell you which side broke (the fixture's
// `logs` ringbuffer + per-scenario assertion messages, not stub state).

import { spawn, type ChildProcess, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";

export interface FixtureBinaries {
  server: string;
  chatd: string;
}

export interface ServerFixture {
  baseUrl: string;
  port: number;
  inviteCode: string;
  jwtSecret: string;
  binaries: FixtureBinaries;
  workDir: string;
  // Server logs (stdout+stderr interleaved). Read on failure for triage.
  readonly logs: string[];
  stop: () => Promise<void>;
  restart: () => Promise<void>;
  cleanup: () => Promise<void>;
}

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");

function pickFreePort(): Promise<number> {
  return new Promise((res, rej) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", rej);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (addr === null || typeof addr === "string") {
        srv.close();
        rej(new Error("could not pick free port"));
        return;
      }
      const port = addr.port;
      srv.close(() => {
        res(port);
      });
    });
  });
}

let cachedBinaries: FixtureBinaries | null = null;

// One-shot build per process — avoids rebuilding for every scenario.
export function buildBinaries(): FixtureBinaries {
  if (cachedBinaries !== null) return cachedBinaries;
  const binDir = join(repoRoot, "bin");
  const server = join(binDir, "server");
  const chatd = join(binDir, "chatd");
  // `go build` is idempotent — if the binary is fresh it's a no-op. We always
  // rebuild so the e2e suite picks up local Go changes without a manual step.
  const goEnv = { ...process.env };
  const r1 = spawnSync("go", ["build", "-o", server, "./apps/server"], {
    cwd: repoRoot,
    env: goEnv,
    stdio: "inherit",
  });
  if (r1.status !== 0) throw new Error("go build apps/server failed");
  const r2 = spawnSync("go", ["build", "-o", chatd, "./apps/cli"], {
    cwd: repoRoot,
    env: goEnv,
    stdio: "inherit",
  });
  if (r2.status !== 0) throw new Error("go build apps/cli failed");
  if (!existsSync(server) || !existsSync(chatd)) {
    throw new Error("expected binaries not present after build");
  }
  cachedBinaries = { server, chatd };
  return cachedBinaries;
}

async function waitForListening(baseUrl: string, timeoutMs = 8000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      // /debug/subs is mounted unconditionally and answers fast; any 2xx/4xx
      // proves the listener is up. Network errors mean the port isn't open
      // yet.
      const r = await fetch(baseUrl + "/debug/subs?channel=__probe__");
      if (r.status > 0) return;
    } catch (e) {
      lastErr = e;
    }
    await new Promise((res) => setTimeout(res, 100));
  }
  throw new Error(
    `server did not accept connections at ${baseUrl} within ${String(timeoutMs)}ms: ${String(lastErr)}`,
  );
}

interface ProcessHandle {
  child: ChildProcess;
  exited: Promise<number | null>;
}

function startServer(opts: {
  binary: string;
  port: number;
  dbPath: string;
  jwtSecret: string;
  inviteCode: string;
  logs: string[];
}): ProcessHandle {
  const child = spawn(opts.binary, [], {
    env: {
      ...process.env,
      CHAT_LISTEN_ADDR: `127.0.0.1:${String(opts.port)}`,
      CHAT_DB_PATH: opts.dbPath,
      CHAT_JWT_SECRET: opts.jwtSecret,
      CHAT_INVITE_CODE: opts.inviteCode,
      // Per-IP register limiter override (issue #114). Production stays at
      // Burst=5 / Refill=15min; the harness needs a generous budget so a
      // single 127.0.0.1 can run many flows back-to-back without hitting
      // 429 rate_limited. Anything large enough to clear a full suite.
      CHAT_REGISTER_BURST: "1000",
      CHAT_REGISTER_REFILL: "1s",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const onLine = (chunk: Buffer): void => {
    const txt = chunk.toString("utf8");
    opts.logs.push(txt);
    // Cap to last 500 entries to keep memory bounded for long suites.
    if (opts.logs.length > 500) opts.logs.splice(0, opts.logs.length - 500);
  };
  child.stdout.on("data", onLine);
  child.stderr.on("data", onLine);
  const exited = new Promise<number | null>((res) => {
    child.once("exit", (code) => {
      res(code);
    });
  });
  return { child, exited };
}

async function stopProcess(p: ProcessHandle): Promise<void> {
  if (p.child.exitCode !== null) return;
  p.child.kill("SIGTERM");
  const winner = await Promise.race([
    p.exited,
    new Promise<"timeout">((res) => {
      setTimeout(() => {
        res("timeout");
      }, 3000);
    }),
  ]);
  if (winner === "timeout") {
    p.child.kill("SIGKILL");
    await p.exited;
  }
}

export async function startFixture(): Promise<ServerFixture> {
  const binaries = buildBinaries();
  const workDir = mkdtempSync(join(tmpdir(), "hackathon-e2e-"));
  const dbPath = join(workDir, "chat.db");
  // 32 bytes = the server's enforced floor (validated at startup). Marked
  // explicitly as a fake test secret so a leaked log line cannot be confused
  // with a real production credential.
  const jwtSecret = "e2e-test-fake-jwt-" + randomBytes(24).toString("hex");
  const inviteCode = "e2e-test-fake-invite-" + randomBytes(8).toString("hex");
  const port = await pickFreePort();
  const baseUrl = `http://127.0.0.1:${String(port)}`;
  const logs: string[] = [];

  let proc = startServer({ binary: binaries.server, port, dbPath, jwtSecret, inviteCode, logs });
  await waitForListening(baseUrl);

  const fixture: ServerFixture = {
    baseUrl,
    port,
    inviteCode,
    jwtSecret,
    binaries,
    workDir,
    logs,
    async stop() {
      await stopProcess(proc);
    },
    async restart() {
      await stopProcess(proc);
      proc = startServer({ binary: binaries.server, port, dbPath, jwtSecret, inviteCode, logs });
      await waitForListening(baseUrl);
    },
    async cleanup() {
      await stopProcess(proc);
      try {
        rmSync(workDir, { recursive: true, force: true });
      } catch {
        // tmpdir cleanup is best-effort; the OS reclaims it eventually.
      }
    },
  };
  return fixture;
}
