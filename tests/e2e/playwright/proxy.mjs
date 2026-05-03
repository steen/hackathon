// A small HTTP proxy that serves the apps/web Vite-built `dist/` on one
// port and forwards `/api/*` and `/ws` to the fixture's Go server. The
// browser then sees same-origin requests, sidestepping CORS without a
// production CORS middleware.
//
// Used by runWeb.mjs:
//   1. start fixture (Go server) -> baseUrl
//   2. `vite build` apps/web        -> dist/
//   3. start this proxy on port 5174, target=fixture for /api+/ws,
//      static-serve for everything else
//   4. exec `playwright test`; tests use the proxy URL as baseURL.

import { createServer, request as httpRequest } from "node:http";
import { createServer as createNetServer } from "node:net";
import { createReadStream, statSync, existsSync } from "node:fs";
import { extname, join, normalize } from "node:path";
import { URL } from "node:url";

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".mjs": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".map": "application/json",
};

function mime(p) {
  return MIME[extname(p).toLowerCase()] ?? "application/octet-stream";
}

function safeJoin(root, urlPath) {
  // Normalize and forbid escape via "..". urlPath has been URL-decoded.
  const cleaned = normalize("/" + urlPath).replace(/^\/+/, "");
  const candidate = join(root, cleaned);
  if (!candidate.startsWith(root)) return null;
  return candidate;
}

function pickFreePort() {
  return new Promise((res, rej) => {
    const srv = createNetServer();
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

function startProxy({ distDir, fixtureBaseUrl }) {
  const fixture = new URL(fixtureBaseUrl);
  const fixtureHost = fixture.hostname;
  const fixturePort = fixture.port ? Number.parseInt(fixture.port, 10) : 80;

  const handleProxy = (req, res) => {
    // Rewriting Host to the fixture address leaves Origin = proxy URL on
    // the upgrade path, so the Go server's same-origin check would 403.
    // runWeb.mjs sets CHAT_ALLOWED_ORIGINS=<proxy origin> to extend
    // OriginPatterns; if you change that wiring, expect cross-origin 403s.
    const opts = {
      host: fixtureHost,
      port: fixturePort,
      method: req.method,
      path: req.url,
      headers: { ...req.headers, host: `${fixtureHost}:${String(fixturePort)}` },
    };
    const upstream = httpRequest(opts, (upRes) => {
      res.writeHead(upRes.statusCode ?? 502, upRes.headers);
      upRes.pipe(res);
    });
    upstream.on("error", (err) => {
      res.writeHead(502, { "content-type": "text/plain" });
      res.end("proxy error: " + String(err.message));
    });
    req.pipe(upstream);
  };

  const handleStatic = (req, res) => {
    const url = new URL(req.url ?? "/", "http://x");
    let target = safeJoin(distDir, decodeURIComponent(url.pathname));
    if (target === null) {
      res.writeHead(403);
      res.end();
      return;
    }
    let st;
    try {
      st = statSync(target);
    } catch {
      st = null;
    }
    if (st !== null && st.isDirectory()) {
      target = join(target, "index.html");
      try {
        st = statSync(target);
      } catch {
        st = null;
      }
    }
    if (st === null) {
      // SPA fallback for hash-routing — serve index.html so the client
      // can hydrate routes from `location.hash`.
      target = join(distDir, "index.html");
      if (!existsSync(target)) {
        res.writeHead(404);
        res.end();
        return;
      }
    }
    res.writeHead(200, { "content-type": mime(target) });
    createReadStream(target).pipe(res);
  };

  const server = createServer((req, res) => {
    const u = req.url ?? "/";
    if (u.startsWith("/api/") || u === "/ws" || u.startsWith("/ws?") || u.startsWith("/debug/")) {
      handleProxy(req, res);
      return;
    }
    handleStatic(req, res);
  });

  // WebSocket upgrade -> forward to fixture.
  server.on("upgrade", (req, sock, head) => {
    const opts = {
      host: fixtureHost,
      port: fixturePort,
      method: req.method,
      path: req.url,
      headers: { ...req.headers, host: `${fixtureHost}:${String(fixturePort)}` },
    };
    const upstream = httpRequest(opts);
    upstream.on("upgrade", (upRes, upSock, upHead) => {
      const lines = [`HTTP/1.1 ${String(upRes.statusCode ?? 101)} Switching Protocols`];
      for (const [k, v] of Object.entries(upRes.headers)) {
        if (Array.isArray(v)) for (const vv of v) lines.push(`${k}: ${String(vv)}`);
        else if (v !== undefined) lines.push(`${k}: ${String(v)}`);
      }
      sock.write(lines.join("\r\n") + "\r\n\r\n");
      if (upHead.length > 0) sock.write(upHead);
      upSock.pipe(sock);
      sock.pipe(upSock);
    });
    upstream.on("error", () => {
      try {
        sock.destroy();
      } catch {
        /* ignore */
      }
    });
    upstream.end(head);
  });

  return server;
}

export { startProxy, pickFreePort };
