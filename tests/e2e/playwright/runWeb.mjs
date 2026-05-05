// Boots the Go server fixture, builds apps/web, starts a tiny same-origin
// proxy that fronts the built `dist/` and forwards `/api/*` + `/ws` to the
// fixture, then execs `playwright test`. Same-origin sidesteps CORS without
// a production middleware change.
//
// Lifecycle:
//   1. go build apps/server, vite build apps/web -> dist/
//   2. pick proxy port + fixture port (free TCP)
//   3. spawn server with CHAT_ALLOWED_ORIGINS=<proxy origin>
//   4. start proxy
//   5. exec `playwright test` with PW_BASE_URL=proxy, teardown on exit

/* global console, fetch, setTimeout */

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";
import { createServer } from "node:net";
import process from "node:process";

import { startProxy, pickFreePort } from "./proxy.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");

function pickFreeServerPort() {
  return new Promise((res, rej) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", rej);
    srv.listen(0, "127.0.0.1", () => {
      const a = srv.address();
      if (a === null || typeof a === "string") {
        srv.close();
        rej(new Error("no port"));
        return;
      }
      const p = a.port;
      srv.close(() => {
        res(p);
      });
    });
  });
}

async function waitForListening(baseUrl, timeoutMs = 8000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(baseUrl + "/debug/subs?channel=__probe__");
      if (r.status > 0) return;
    } catch {
      /* not yet */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`server not up at ${baseUrl}`);
}

async function main() {
  const binDir = join(repoRoot, "bin");
  const serverBin = join(binDir, "server");

  console.log("[e2e:web] building Go server...");
  const r = spawnSync("go", ["build", "-o", serverBin, "./apps/server"], {
    cwd: repoRoot,
    stdio: "inherit",
  });
  if (r.status !== 0 || !existsSync(serverBin)) {
    console.error("[e2e:web] go build apps/server failed");
    process.exit(1);
  }

  // Build with --mode e2e (via the build:e2e script) so the
  // window.__chatd WS-transition hook in main.tsx — gated on MODE !==
  // "production" — stays installed in the bundle Playwright loads. A
  // plain `vite build` defaults to MODE=production and would strip it,
  // breaking the WS-drops assertions in web.spec.ts. See #658.
  console.log("[e2e:web] building apps/web (vite build --mode e2e)...");
  const b = spawnSync("pnpm", ["--filter", "web", "run", "build:e2e"], {
    cwd: repoRoot,
    stdio: "inherit",
    env: { ...process.env },
  });
  if (b.status !== 0) {
    console.error("[e2e:web] pnpm --filter web run build:e2e failed");
    process.exit(1);
  }
  const distDir = resolve(repoRoot, "apps", "web", "dist");
  if (!existsSync(distDir)) {
    console.error("[e2e:web] dist/ missing after build");
    process.exit(1);
  }

  const workDir = mkdtempSync(join(tmpdir(), "hackathon-e2e-web-"));
  const dbPath = join(workDir, "chat.db");
  const jwtSecret = "e2e-test-fake-jwt-" + randomBytes(24).toString("hex");
  const inviteCode = "e2e-test-fake-invite-" + randomBytes(8).toString("hex");

  const fixturePort = await pickFreeServerPort();
  const proxyPort = await pickFreePort();
  const fixtureBaseUrl = `http://127.0.0.1:${String(fixturePort)}`;
  const webBaseUrl = `http://127.0.0.1:${String(proxyPort)}`;

  // Start fixture with the proxy origin allowlisted so the WS upgrade
  // check (server-internal/wsapi.Handler reads OriginPatterns) accepts
  // the browser's Origin header (= proxy URL).
  console.log(`[e2e:web] starting fixture on ${fixtureBaseUrl}, allow=${webBaseUrl}`);
  const server = spawn(serverBin, [], {
    env: {
      ...process.env,
      CHAT_SERVER_PORT: String(fixturePort),
      CHAT_DB_PATH: dbPath,
      CHAT_JWT_SECRET: jwtSecret,
      CHAT_INVITE_CODE: inviteCode,
      CHAT_ALLOWED_ORIGINS: webBaseUrl,
      // Production default is Burst=5/15min (PRD §9). Every Playwright test
      // here registers 1-2 fresh users from 127.0.0.1, so the suite exhausts
      // the bucket once it grows past four tests — and CI's retries=1 amps
      // that further. Loosen the per-IP register limit for the fixture only;
      // ratelimit.RegisterIPConfigFromEnv reads CHAT_REGISTER_BURST and logs
      // a WARN so a stray production override would still surface loudly.
      CHAT_REGISTER_BURST: "200",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  server.stdout.on("data", (b) => {
    process.stdout.write("[server] " + b.toString("utf8"));
  });
  server.stderr.on("data", (b) => {
    process.stderr.write("[server] " + b.toString("utf8"));
  });
  await waitForListening(fixtureBaseUrl);

  console.log(`[e2e:web] starting proxy on ${webBaseUrl}, target=${fixtureBaseUrl}`);
  const proxy = startProxy({ distDir, fixtureBaseUrl });
  await new Promise((res, rej) => {
    proxy.once("error", rej);
    proxy.listen(proxyPort, "127.0.0.1", () => {
      res();
    });
  });

  const cleanup = () => {
    try {
      proxy.close();
    } catch {
      /* ignore */
    }
    try {
      if (server.exitCode === null) server.kill("SIGTERM");
    } catch {
      /* ignore */
    }
    try {
      rmSync(workDir, { recursive: true, force: true });
    } catch {
      /* ignore */
    }
  };

  process.on("SIGINT", () => {
    cleanup();
    process.exit(130);
  });
  process.on("SIGTERM", () => {
    cleanup();
    process.exit(143);
  });

  const args = process.argv.slice(2);
  console.log("[e2e:web] running playwright test...");
  const playwright = spawn(
    "pnpm",
    ["--filter", "web", "exec", "playwright", "test", "--config", "playwright.config.ts", ...args],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        E2E_BASE_URL: fixtureBaseUrl,
        E2E_INVITE_CODE: inviteCode,
        PW_BASE_URL: webBaseUrl,
      },
      stdio: "inherit",
    },
  );
  const code = await new Promise((res) => {
    playwright.once("exit", (c) => {
      res(c);
    });
  });

  cleanup();
  process.exit(code ?? 1);
}

main().catch((err) => {
  console.error("[e2e:web] fatal:", err);
  process.exit(1);
});
