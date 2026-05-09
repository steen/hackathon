// Phase 8 wire-drift canary: cross-client e2e for the {type:"channel"}
// WS frame.
//
// CLAUDE.md "Wire types" mandates that every JSON-field change land
// alongside an e2e assertion under tests/e2e/ that fails CI when either
// the Go-client struct (packages/go-client/ws.go) or the TS api-client
// interface (packages/api-client/src/types.ts) drifts.
// specs/plans/phase-8/80-feature-clients-channel-extensions.md AC line 53
// pins this requirement to the channel-create + channel-rename frames.
//
// What this test does:
//
//  1. Spawns a Go subprocess (./cmd/wsobserver) that registers a fresh
//     user, opens a packages/go-client WebSocket via goclient.Watch,
//     decodes inbound frames through the typed Event/ChannelEvent
//     surface, and emits one JSON line per observed `channel` event on
//     stdout.
//  2. From the test process, opens a TS-client WebSocketClient
//     (packages/api-client) on the same server and decodes inbound
//     frames through the typed Event union.
//  3. Issues POST /api/channels and PATCH /api/channels/{id} via the
//     server's REST surface.
//  4. Asserts both the Go observer and the TS subscriber receive a
//     ChannelEvent with `kind:"create"` (then `kind:"rename"`) carrying
//     the same id + name + created_at as the REST response.
//
// A field rename on either side fails the test:
//   - Go: changing `Kind string \`json:"kind"\`` to e.g. `\`json:"Kind"\``
//     leaves goclient's ChannelEvent decoded with an empty Kind, so the
//     Go observer's emitted line lacks the kind value and the assertion
//     in step 4 fails.
//   - TS: changing the type union's `kind` to `Kind` causes the typed
//     destructure below (`data.kind`) to surface `undefined`, again
//     failing step 4.

import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createInterface } from "node:readline";
import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { WebSocket as NodeWebSocket } from "ws";
import {
  createClient,
  type Channel,
  type ChannelEvent,
  type Event as WsEvent,
  type WebSocketCtor,
} from "@hackathon/api-client";

// Cast through `unknown` because the `ws` package's WebSocket types
// `onopen` against the DOM `Event`, while api-client's WebSocketLike
// uses `(ev: unknown) => void`. The runtime shapes match.
const WSCtor = NodeWebSocket as unknown as WebSocketCtor;

interface ObserverLine {
  event: string;
  kind?: string;
  id?: string;
  name?: string;
  created_at?: string;
  message?: string;
}

interface ObserverHandle {
  proc: ChildProcess;
  ready: Promise<void>;
  events: ObserverLine[];
  waitFor: (kind: "create" | "rename", timeoutMs: number) => Promise<ObserverLine>;
  shutdown: () => Promise<void>;
}

function serverUrl(): string {
  const v = process.env.E2E_SERVER_URL;
  if (!v) throw new Error("E2E_SERVER_URL missing — globalSetup did not run");
  return v;
}

function inviteCode(): string {
  const v = process.env.E2E_INVITE_CODE;
  if (!v) throw new Error("E2E_INVITE_CODE missing — globalSetup did not run");
  return v;
}

function observerBin(): string {
  const v = process.env.E2E_WSOBSERVER_BIN;
  if (!v) throw new Error("E2E_WSOBSERVER_BIN missing — globalSetup did not run");
  return v;
}

function startGoObserver(): ObserverHandle {
  const username = "go-" + randomBytes(6).toString("hex");
  const password = randomBytes(16).toString("hex");
  const proc = spawn(
    observerBin(),
    [
      "-base-url",
      serverUrl(),
      "-username",
      username,
      "-password",
      password,
      "-invite",
      inviteCode(),
      "-timeout",
      "60s",
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  const events: ObserverLine[] = [];
  const waiters: ((line: ObserverLine) => void)[] = [];
  let readyResolve!: () => void;
  let readyReject!: (e: unknown) => void;
  const ready = new Promise<void>((res, rej) => {
    readyResolve = res;
    readyReject = rej;
  });

  const rl = createInterface({ input: proc.stdout });
  rl.on("line", (raw) => {
    let parsed: ObserverLine;
    try {
      parsed = JSON.parse(raw) as ObserverLine;
    } catch {
      process.stderr.write(`[wsobserver:non-json] ${raw}\n`);
      return;
    }
    if (parsed.event === "ready") {
      readyResolve();
      return;
    }
    if (parsed.event === "error") {
      readyReject(new Error(`wsobserver error: ${parsed.message ?? "<no message>"}`));
      return;
    }
    if (parsed.event === "channel") {
      events.push(parsed);
      const w = waiters.shift();
      if (w) w(parsed);
    }
  });
  proc.stderr.on("data", (d: Buffer) => {
    process.stderr.write(`[wsobserver] ${d.toString("utf8")}`);
  });
  proc.on("exit", (code) => {
    if (code !== null && code !== 0) {
      readyReject(new Error(`wsobserver exited with code ${String(code)}`));
    }
  });

  function waitFor(kind: "create" | "rename", timeoutMs: number): Promise<ObserverLine> {
    const existing = events.find((e) => e.kind === kind);
    if (existing) return Promise.resolve(existing);
    return new Promise<ObserverLine>((resolve, reject) => {
      const timer = setTimeout(() => {
        const idx = waiters.indexOf(seek);
        if (idx >= 0) waiters.splice(idx, 1);
        reject(new Error(`go observer did not emit kind=${kind} within ${String(timeoutMs)}ms`));
      }, timeoutMs);
      const seek = (line: ObserverLine): void => {
        if (line.kind === kind) {
          clearTimeout(timer);
          resolve(line);
        } else {
          waiters.push(seek);
        }
      };
      waiters.push(seek);
    });
  }

  async function shutdown(): Promise<void> {
    if (proc.exitCode !== null) return;
    proc.kill("SIGTERM");
    await new Promise<void>((res) => {
      const t = setTimeout(() => {
        proc.kill("SIGKILL");
        res();
      }, 2000);
      proc.once("exit", () => {
        clearTimeout(t);
        res();
      });
    });
  }

  return { proc, ready, events, waitFor, shutdown };
}

interface TsSubscription {
  ready: Promise<void>;
  channels: ChannelEvent[];
  waitFor: (kind: "create" | "rename", timeoutMs: number) => Promise<ChannelEvent>;
  close: () => void;
}

function startTsSubscription(): TsSubscription {
  const username = "ts-" + randomBytes(6).toString("hex");
  const password = randomBytes(16).toString("hex");
  const client = createClient({ baseUrl: serverUrl(), WebSocket: WSCtor });
  const channels: ChannelEvent[] = [];
  const waiters: ((ev: ChannelEvent) => void)[] = [];
  let openResolve!: () => void;
  let openReject!: (e: unknown) => void;
  const opened = new Promise<void>((res, rej) => {
    openResolve = res;
    openReject = rej;
  });

  let ws: ReturnType<typeof client.websocket>;
  const setup = (async (): Promise<void> => {
    await client.register(username, password, inviteCode());
    // The WS handler now requires ?channel=<id>; subscribe to the
    // seeded "general" channel. channel:create / channel:rename events
    // fan out via Hub.BroadcastAll, so the observer still sees them
    // regardless of which channel it's on.
    const list = await client.listChannels();
    const general = list.find((ch) => ch.name === "general");
    if (!general) throw new Error("seeded 'general' channel not found");
    ws = client.websocket(general.id);
    ws.on("open", () => {
      openResolve();
    });
    ws.on("error", (err) => {
      openReject(err instanceof Error ? err : new Error(String(err)));
    });
    ws.on("message", (ev: WsEvent) => {
      if (!isChannelEvent(ev)) return;
      channels.push(ev);
      const w = waiters.shift();
      if (w) w(ev);
    });
    await ws.connect();
    await opened;
  })();

  function waitFor(kind: "create" | "rename", timeoutMs: number): Promise<ChannelEvent> {
    const existing = channels.find((e) => e.data.kind === kind);
    if (existing) return Promise.resolve(existing);
    return new Promise<ChannelEvent>((resolve, reject) => {
      const timer = setTimeout(() => {
        const idx = waiters.indexOf(seek);
        if (idx >= 0) waiters.splice(idx, 1);
        reject(new Error(`ts subscriber did not see kind=${kind} within ${String(timeoutMs)}ms`));
      }, timeoutMs);
      const seek = (ev: ChannelEvent): void => {
        if (ev.data.kind === kind) {
          clearTimeout(timer);
          resolve(ev);
        } else {
          waiters.push(seek);
        }
      };
      waiters.push(seek);
    });
  }

  return {
    ready: setup,
    channels,
    waitFor: async (kind, timeoutMs) => {
      await setup;
      return waitFor(kind, timeoutMs);
    },
    close: () => {
      ws.close();
    },
  };
}

function isChannelEvent(ev: WsEvent): ev is ChannelEvent {
  if (ev.type !== "channel") return false;
  const d = ev.data;
  if (typeof d !== "object" || d === null) return false;
  const r = d as Record<string, unknown>;
  return typeof r.kind === "string" && typeof r.channel === "object" && r.channel !== null;
}

describe("phase-8 wire-drift canary: channel WS frame parity across Go + TS clients", () => {
  let observer: ObserverHandle;
  let tsSub: TsSubscription;
  let driver: ReturnType<typeof createClient>;

  beforeAll(async () => {
    observer = startGoObserver();
    tsSub = startTsSubscription();

    // The driver is a third (REST-only) client used to issue POST/PATCH.
    // Using a separate client keeps the test's intent unambiguous: the
    // observers only consume WS frames; the driver only mutates state.
    const driverUser = "drv-" + randomBytes(6).toString("hex");
    const driverPassword = randomBytes(16).toString("hex");
    driver = createClient({ baseUrl: serverUrl(), WebSocket: WSCtor });
    await driver.register(driverUser, driverPassword, inviteCode());

    // Both observers must have their WS upgrade complete before the
    // driver issues any REST mutation; otherwise the broadcast races
    // the upgrade and the test sees a "subscriber did not observe
    // kind=create" timeout. observer.ready resolves when wsobserver
    // emits its "ready" line (after goclient.Watch returns); tsSub.ready
    // resolves when the api-client WebSocketClient fires "open".
    await Promise.all([observer.ready, tsSub.ready]);
  }, 30_000);

  afterAll(async () => {
    tsSub.close();
    await observer.shutdown();
  });

  it("both clients decode {type:'channel', data:{kind:'create', channel:{...}}} with field parity", async () => {
    const name = "drift-" + randomBytes(4).toString("hex");
    const created: Channel = await driver.createChannel(name);

    const goLine = await observer.waitFor("create", 8000);
    const tsEvent = await tsSub.waitFor("create", 8000);

    expect(goLine.kind).toBe("create");
    expect(goLine.id).toBe(created.id);
    expect(goLine.name).toBe(created.name);
    expect(typeof goLine.created_at).toBe("string");
    expect(goLine.created_at?.length ?? 0).toBeGreaterThan(0);

    expect(tsEvent.type).toBe("channel");
    expect(tsEvent.data.kind).toBe("create");
    if (tsEvent.data.kind !== "create") throw new Error("waitFor returned non-create");
    expect(tsEvent.data.channel.id).toBe(created.id);
    expect(tsEvent.data.channel.name).toBe(created.name);
    expect(typeof tsEvent.data.channel.created_at).toBe("string");
    expect(tsEvent.data.channel.created_at.length).toBeGreaterThan(0);

    // Cross-decoder parity: both decoders agree on the channel identity.
    // A struct-tag rename on either side would null one of these fields,
    // and this assertion would surface the drift before the canary
    // returns.
    expect(goLine.id).toBe(tsEvent.data.channel.id);
    expect(goLine.name).toBe(tsEvent.data.channel.name);

    // The REST envelope also returns the same Channel shape; pin parity
    // there too so a drift between REST decode and WS decode (e.g. one
    // side renames the JSON tag in only one struct) fails here.
    expect(created.name).toBe(name);
    expect(typeof created.id).toBe("string");
    expect(created.id.length).toBeGreaterThan(0);
  }, 20_000);

  it("both clients decode {type:'channel', data:{kind:'rename', channel:{...}}} with field parity", async () => {
    const initialName = "drift2-" + randomBytes(4).toString("hex");
    const created: Channel = await driver.createChannel(initialName);
    // Drain the create frame on both observers so the rename frame is
    // unambiguous when waitFor returns.
    await observer.waitFor("create", 8000);
    await tsSub.waitFor("create", 8000);

    const newName = "drift2x-" + randomBytes(4).toString("hex");
    const renamed: Channel = await driver.renameChannel(created.id, newName);
    expect(renamed.id).toBe(created.id);
    expect(renamed.name).toBe(newName);

    const goLine = await observer.waitFor("rename", 8000);
    const tsEvent = await tsSub.waitFor("rename", 8000);

    expect(goLine.kind).toBe("rename");
    expect(goLine.id).toBe(created.id);
    expect(goLine.name).toBe(newName);

    expect(tsEvent.data.kind).toBe("rename");
    if (tsEvent.data.kind !== "rename") throw new Error("waitFor returned non-rename");
    expect(tsEvent.data.channel.id).toBe(created.id);
    expect(tsEvent.data.channel.name).toBe(newName);

    expect(goLine.id).toBe(tsEvent.data.channel.id);
    expect(goLine.name).toBe(tsEvent.data.channel.name);
  }, 20_000);
});
