// vitest globalSetup that builds apps/server, picks a free port,
// spawns the binary with random per-suite secrets and a tempdir
// SQLite, polls readiness, and exposes E2E_SERVER_URL +
// E2E_INVITE_CODE via process.env so worker processes can read them.
//
// Teardown SIGTERMs and waits for exit (with SIGKILL fallback after
// 5s).

import { spawn, type ChildProcess, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..", "..");

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

async function waitForListening(baseUrl: string, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(baseUrl + "/debug/subs?channel=__probe__");
      if (r.status > 0) return;
    } catch (e) {
      lastErr = e;
    }
    await new Promise((res) => setTimeout(res, 100));
  }
  throw new Error(
    `server did not listen on ${baseUrl} within ${String(timeoutMs)}ms: ${String(lastErr)}`,
  );
}

let child: ChildProcess | null = null;
let workDir: string | null = null;
let exitedPromise: Promise<number | null> | null = null;

export default async function setup(): Promise<() => Promise<void>> {
  // Build the Go server binary into a tempdir so this run never fights
  // a parallel build for the same output path.
  workDir = mkdtempSync(join(tmpdir(), "ts-api-client-e2e-"));
  const serverBin = join(workDir, "chat-server");
  const build = spawnSync("go", ["build", "-o", serverBin, "./apps/server"], {
    cwd: repoRoot,
    stdio: "inherit",
  });
  if (build.status !== 0) {
    throw new Error("go build ./apps/server failed");
  }
  if (!existsSync(serverBin)) {
    throw new Error(`server binary missing at ${serverBin}`);
  }

  const port = await pickFreePort();
  const baseUrl = `http://127.0.0.1:${String(port)}`;
  const dbPath = join(workDir, "chat.db");
  // 32 bytes = 64 hex chars. Server requires >= 32-byte secret.
  const jwtSecret = randomBytes(32).toString("hex");
  // 16 bytes = 32 hex chars (well above 8-byte floor).
  const inviteCode = randomBytes(16).toString("hex");

  child = spawn(serverBin, [], {
    env: {
      ...process.env,
      CHAT_LISTEN_ADDR: `127.0.0.1:${String(port)}`,
      CHAT_DB_PATH: dbPath,
      CHAT_JWT_SECRET: jwtSecret,
      CHAT_INVITE_CODE: inviteCode,
      // Loopback E2E hits the per-IP register limiter fast; bump it
      // so a full suite of register calls clears.
      CHAT_REGISTER_BURST: "1000",
      CHAT_REGISTER_REFILL: "1s",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout?.on("data", (d: Buffer) => {
    process.stderr.write(`[server] ${d.toString("utf8")}`);
  });
  child.stderr?.on("data", (d: Buffer) => {
    process.stderr.write(`[server] ${d.toString("utf8")}`);
  });
  exitedPromise = new Promise<number | null>((res) => {
    child?.once("exit", (code) => {
      res(code);
    });
  });

  await waitForListening(baseUrl);

  process.env.E2E_SERVER_URL = baseUrl;
  process.env.E2E_INVITE_CODE = inviteCode;
  process.env.E2E_JWT_SECRET = jwtSecret;

  return async function teardown(): Promise<void> {
    if (child?.exitCode === null) {
      child.kill("SIGTERM");
      const winner = await Promise.race([
        exitedPromise,
        new Promise<"timeout">((res) => {
          setTimeout(() => {
            res("timeout");
          }, 5000);
        }),
      ]);
      if (winner === "timeout") {
        child.kill("SIGKILL");
        await exitedPromise;
      }
    }
    if (workDir) {
      try {
        rmSync(workDir, { recursive: true, force: true });
      } catch {
        // best-effort
      }
    }
  };
}
