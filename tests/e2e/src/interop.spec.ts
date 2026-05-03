// Cross-client interop: CLI <-> web-style WS subscriber, and same-user
// multi-connection collapse. The "web-style" subscriber here is a Node
// WebSocket talking to the same /ws endpoint the api-client uses; the
// browser-driven scenarios live in playwright/web.spec.ts.

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import WebSocket from "ws";
import { startFixture, type ServerFixture } from "./serverFixture.js";
import { newCliSession } from "./cliRunner.js";
import {
  createChannel,
  loginUser,
  postMessage,
  registerUser,
  listPresence,
  uniqueUsername,
} from "./restClient.js";

const TEST_PASSWORD = "e2e-fake-pw-1234567890";

interface WsSession {
  ws: WebSocket;
  messages: { type: string; data?: unknown }[];
  close: () => Promise<void>;
}

async function openWs(opts: {
  baseUrl: string;
  token: string;
  channel?: string;
}): Promise<WsSession> {
  const ticketRes = await fetch(opts.baseUrl + "/api/auth/ws-ticket", {
    method: "POST",
    headers: { Authorization: `Bearer ${opts.token}` },
  });
  const env = (await ticketRes.json()) as { ok: boolean; data?: { ticket: string } };
  if (!env.ok || !env.data?.ticket) throw new Error("ticket mint failed: " + JSON.stringify(env));
  const url = new URL(opts.baseUrl);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = "/ws";
  url.searchParams.set("ticket", env.data.ticket);
  if (opts.channel) url.searchParams.set("channel", opts.channel);

  const ws = new WebSocket(url.toString());
  const messages: { type: string; data?: unknown }[] = [];
  ws.on("message", (raw: WebSocket.RawData) => {
    try {
      const text = Buffer.isBuffer(raw)
        ? raw.toString("utf8")
        : Array.isArray(raw)
          ? Buffer.concat(raw).toString("utf8")
          : Buffer.from(raw).toString("utf8");
      const parsed = JSON.parse(text) as { type?: string; data?: unknown };
      if (typeof parsed.type === "string") {
        messages.push({ type: parsed.type, data: parsed.data });
      }
    } catch {
      // server only sends JSON frames
    }
  });
  await new Promise<void>((res, rej) => {
    ws.once("open", () => {
      res();
    });
    ws.once("error", (e: Error) => {
      rej(e);
    });
  });
  return {
    ws,
    messages,
    async close() {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        await new Promise<void>((res) => {
          ws.once("close", () => {
            res();
          });
          ws.close();
        });
      }
    },
  };
}

async function waitFor(check: () => boolean | Promise<boolean>, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`condition not met within ${String(timeoutMs)}ms`);
}

describe("Cross-client interop", () => {
  let fx!: ServerFixture;

  beforeEach(async () => {
    fx = await startFixture();
  });

  afterEach(async () => {
    await fx.cleanup();
  });

  it("AC: a CLI-posted message reaches a WS subscriber (CLI -> web WS)", async () => {
    const cli = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const sender = uniqueUsername("u-cli-sender");
      await cli.run(["register", sender], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      const senderTok = (await loginUser(fx.baseUrl, sender, TEST_PASSWORD)).token;

      // Separate user for the WS subscriber so the test exercises a real
      // cross-client path (different user, different token).
      const subUser = uniqueUsername("u-ws-sub");
      const subReg = await registerUser(fx.baseUrl, fx.inviteCode, subUser, TEST_PASSWORD);
      const channel = await createChannel(fx.baseUrl, senderTok, uniqueUsername("ch"));

      const session = await openWs({
        baseUrl: fx.baseUrl,
        token: subReg.token,
        channel: channel.id,
      });
      try {
        const body = `cli-to-ws-${String(Date.now())}`;
        const send = await cli.run(["send", channel.id, body]);
        expect(send.exitCode, send.stderr).toBe(0);

        await waitFor(
          () =>
            session.messages.some(
              (m) => m.type === "message" && JSON.stringify(m.data).includes(body),
            ),
          5000,
        );
      } finally {
        await session.close();
      }
    } finally {
      cli.cleanup();
    }
  });

  it("AC: a WS-posted message (REST) reaches a CLI watcher (web -> CLI)", async () => {
    const cli = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const watcher = uniqueUsername("u-cli-watch-int");
      await cli.run(["register", watcher], {
        env: { CHAT_PASSWORD: TEST_PASSWORD, CHAT_INVITE_CODE: fx.inviteCode },
      });
      const watcherTok = (await loginUser(fx.baseUrl, watcher, TEST_PASSWORD)).token;
      const channel = await createChannel(fx.baseUrl, watcherTok, uniqueUsername("ch"));

      const sender = uniqueUsername("u-rest-sender");
      const senderReg = await registerUser(fx.baseUrl, fx.inviteCode, sender, TEST_PASSWORD);

      const long = cli.spawnLong(["watch", channel.id]);
      try {
        await new Promise((r) => setTimeout(r, 500)); // let the subscribe land
        const body = `web-to-cli-${String(Date.now())}`;
        await postMessage(fx.baseUrl, senderReg.token, channel.id, body);
        await waitFor(() => long.stdout().includes(body), 5000);
        expect(long.stdout()).toContain(body);
      } finally {
        await long.stop();
      }
    } finally {
      cli.cleanup();
    }
  });

  it("AC: same user on multiple connections shows as online once", async () => {
    const cli = newCliSession({ binary: fx.binaries.chatd, baseUrl: fx.baseUrl });
    try {
      const u = uniqueUsername("u-dup-conn");
      const reg = await registerUser(fx.baseUrl, fx.inviteCode, u, TEST_PASSWORD);
      const channel = await createChannel(fx.baseUrl, reg.token, uniqueUsername("ch"));

      // Observer reads /api/presence.
      const obs = uniqueUsername("u-dup-obs");
      const obsReg = await registerUser(fx.baseUrl, fx.inviteCode, obs, TEST_PASSWORD);

      const a = await openWs({ baseUrl: fx.baseUrl, token: reg.token, channel: channel.id });
      const b = await openWs({ baseUrl: fx.baseUrl, token: reg.token, channel: channel.id });
      try {
        await waitFor(async () => {
          const p = await listPresence(fx.baseUrl, obsReg.token);
          return p.users.some((x) => x.id === reg.user.id);
        }, 5000);
        const p = await listPresence(fx.baseUrl, obsReg.token);
        const matches = p.users.filter((x) => x.id === reg.user.id);
        expect(matches.length).toBe(1);
      } finally {
        await a.close();
        await b.close();
      }
    } finally {
      cli.cleanup();
    }
  });
});
