import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve as resolvePath } from "node:path";

class FakeSocket {
  static instances: FakeSocket[] = [];
  url: string;
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.(undefined);
  }
  forceClose(code = 1006, reason = "abnormal"): void {
    this.readyState = 3;
    this.onclose?.({ code, reason });
  }
  send(data: string): void {
    this.sent.push(data);
  }
  close(): void {
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: "normal" });
  }
}

const listChannelsMock = vi.fn();
const listMessagesMock = vi.fn();
const postMessageMock = vi.fn();
const meMock = vi.fn();
const logoutMock = vi.fn();
const wsTicketMock = vi.fn();
const httpRequestMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    listChannels: listChannelsMock,
    listMessages: listMessagesMock,
    postMessage: postMessageMock,
    me: meMock,
    logout: logoutMock,
    http: {
      wsTicket: wsTicketMock,
      getBaseUrl: () => "http://test.local",
      request: httpRequestMock,
    },
  }),
  readToken: () => "test-jwt-token-placeholder",
  writeToken: vi.fn(),
}));

import { AuthProvider } from "../auth/AuthContext.js";
import { Chat } from "./Chat.js";

beforeEach(() => {
  (globalThis as { WebSocket?: unknown }).WebSocket = FakeSocket;
});

afterEach(() => {
  cleanup();
  FakeSocket.instances = [];
  delete (globalThis as { WebSocket?: unknown }).WebSocket;
  listChannelsMock.mockReset();
  listMessagesMock.mockReset();
  postMessageMock.mockReset();
  meMock.mockReset();
  logoutMock.mockReset();
  wsTicketMock.mockReset();
  httpRequestMock.mockReset();
});

function happyPath(): void {
  meMock.mockResolvedValue({ id: "U1", username: "alice" });
  listChannelsMock.mockResolvedValue([
    { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
  ]);
  listMessagesMock.mockResolvedValue([
    {
      id: "M1",
      channel_id: "C1",
      sender_user_id: "U2",
      body: "hello from history",
      created_at: "2026-01-01T00:00:00Z",
    },
  ]);
  let n = 0;
  wsTicketMock.mockImplementation(async () => {
    await Promise.resolve();
    n += 1;
    return { ticket: `ticket-${String(n)}`, expires_at: "2026-01-01T01:00:00Z" };
  });
  httpRequestMock.mockImplementation((method: string, path: string) => {
    if (method === "GET" && path === "/api/presence") {
      return Promise.resolve({ users: [] });
    }
    return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
  });
}

describe("test_web_chat_page_renders_history_then_appends_live_messages", () => {
  it("renders history rows then appends a live WS message", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    // The presence hook also opens a (channel-less) socket; pick the
    // messages socket by `channel=` query param.
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));
    expect(sock).toBeDefined();

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M2",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live message",
            created_at: "2026-01-01T00:00:01Z",
          },
        }),
      });
      await Promise.resolve();
    });

    expect(screen.getByText("live message")).toBeInTheDocument();
    const items = screen.getByTestId("message-list").querySelectorAll('[data-testid="msg"]');
    expect(items).toHaveLength(2);
  });
});

describe("test_web_chat_renders_history_in_chronological_order_on_first_load", () => {
  it("renders history rows oldest→newest (composer under the newest)", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    // Server contract: newest-first window. The hook flips this to
    // oldest-first; the DOM must reflect the flipped order.
    listMessagesMock.mockResolvedValue([
      {
        id: "M3",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "third",
        created_at: "2026-01-01T00:00:03Z",
      },
      {
        id: "M2",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "second",
        created_at: "2026-01-01T00:00:02Z",
      },
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "first",
        created_at: "2026-01-01T00:00:01Z",
      },
    ]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("first")).toBeInTheDocument();
    });
    const list = screen.getByTestId("message-list");
    const bodies = Array.from(list.querySelectorAll<HTMLElement>(".msg__body")).map(
      (el) => el.textContent,
    );
    expect(bodies).toEqual(["first", "second", "third"]);
  });
});

describe("test_web_chat_appends_live_messages_below_most_recent_history", () => {
  it("a live WS frame lands below the newest history row", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    listMessagesMock.mockResolvedValue([
      {
        id: "M3",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "third",
        created_at: "2026-01-01T00:00:03Z",
      },
      {
        id: "M2",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "second",
        created_at: "2026-01-01T00:00:02Z",
      },
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "first",
        created_at: "2026-01-01T00:00:01Z",
      },
    ]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("third")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M4",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live",
            created_at: "2026-01-01T00:00:04Z",
          },
        }),
      });
      await Promise.resolve();
    });

    const list = screen.getByTestId("message-list");
    await waitFor(() => {
      const bodies = Array.from(list.querySelectorAll<HTMLElement>(".msg__body")).map(
        (el) => el.textContent,
      );
      expect(bodies).toEqual(["first", "second", "third", "live"]);
    });
  });
});

describe("test_web_reconnects_after_ws_disconnect", () => {
  it("forced close triggers reconnect that mints a fresh ticket", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    // Filter to the messages WS (carries `channel=C1`); the presence
    // hook also opens a channel-less WS that mints its own ticket.
    await waitFor(
      () => {
        expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
      },
      { timeout: 2000 },
    );
    const messageSockets = (): FakeSocket[] =>
      FakeSocket.instances.filter((s) => s.url.includes("channel=C1"));
    const first = messageSockets()[0];
    first?.open();

    first?.forceClose();

    // BACKOFF_MS[0] is 500ms; allow real time to elapse for the api-client's
    // reconnect timer to fire, then mint a fresh ticket and open a new socket.
    await waitFor(
      () => {
        expect(messageSockets().length).toBeGreaterThanOrEqual(2);
      },
      { timeout: 3000 },
    );
    expect(messageSockets()[1]?.url).toContain("channel=C1");
  });
});

describe("test_message_with_html_tags_renders_as_text_not_dom", () => {
  it("renders <script>alert(1)</script> as literal text, not a DOM script", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    listMessagesMock.mockResolvedValue([
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "<script>alert(1)</script>",
        created_at: "2026-01-01T00:00:00Z",
      },
    ]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("<script>alert(1)</script>")).toBeInTheDocument();
    });

    const list = screen.getByTestId("message-list");
    expect(list.querySelector("script")).toBeNull();
    const body = list.querySelector(".msg__body");
    expect(body?.textContent).toBe("<script>alert(1)</script>");
    expect(body?.innerHTML).toContain("&lt;script&gt;");
  });
});

describe("test_web_presence_list_renders_seed_join_leave_and_dedupes", () => {
  it("seeds from /api/presence, applies join/leave WS frames, and dedupes multi-conn joins", async () => {
    happyPath();
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({ users: [{ id: "U1", username: "alice" }] });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const presenceList = await screen.findByTestId("presence-list");
    await waitFor(() => {
      expect(screen.getByTestId("presence-user-U1")).toBeInTheDocument();
    });

    // The presence hook opens its own WebSocket connection in addition
    // to the messages hook's connection — find the presence socket by
    // url query (no `channel=` param).
    const presenceSock = FakeSocket.instances.find((s) => !s.url.includes("channel="));
    expect(presenceSock).toBeDefined();

    await act(async () => {
      presenceSock?.open();
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U2" },
        }),
      });
      // Duplicate join from a second connection of the same user must
      // collapse to one entry.
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U2" },
        }),
      });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(screen.getByTestId("presence-user-U2")).toBeInTheDocument();
    });
    expect(presenceList.querySelectorAll('[data-testid^="presence-user-"]')).toHaveLength(2);

    await act(async () => {
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "leave", user_id: "U1" },
        }),
      });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(screen.queryByTestId("presence-user-U1")).toBeNull();
    });
    expect(screen.getByTestId("presence-user-U2")).toBeInTheDocument();
  });
});

describe("test_web_pending_message_renders_sending_badge_italic_no_opacity", () => {
  it("posts a message and asserts the pending row carries the badge, italic body, and no inline style", async () => {
    // vitest+vite does not inject imported CSS into the jsdom document
    // (the `import "../styles.css"` path returns an empty module here),
    // so read the stylesheet from disk and attach it as a <style> tag.
    // jsdom's CSSOM resolves descendant selectors in getComputedStyle
    // once the rules are present.
    const cssPath = resolvePath(process.cwd(), "src/styles.css");
    const cssText = readFileSync(cssPath, "utf-8");
    const styleEl = document.createElement("style");
    styleEl.dataset.testInjected = "msg-pending";
    styleEl.textContent = cssText;
    document.head.appendChild(styleEl);

    happyPath();
    // Hold postMessage open so the optimistic entry stays in `pending` for
    // the duration of the assertions. Resolving after the test ends keeps
    // the pending state fixed without leaking timers between tests.
    const resolvers: (() => void)[] = [];
    postMessageMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolvers.push(() => {
            resolve();
          });
        }),
    );

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    // Wait for the composer to enable. listChannels and listMessages
    // resolve independently — the input flips from disabled→enabled only
    // after the active channel state is set, which is what gates send().
    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    const input = await screen.findByLabelText<HTMLInputElement>("message");
    await waitFor(() => {
      expect(input.disabled).toBe(false);
    });
    const form = input.closest("form");
    expect(form).not.toBeNull();
    if (form === null) return;

    act(() => {
      fireEvent.change(input, { target: { value: "pending body" } });
    });
    act(() => {
      fireEvent.submit(form);
    });

    await waitFor(() => {
      expect(postMessageMock).toHaveBeenCalledWith("C1", "pending body");
    });

    // AC-1: the Sending… badge is exposed via role="status". Two
    // role="status" elements exist on this page (the connection badge and
    // the pending badge); narrow to the one whose text starts with
    // "Sending" so the assertion fails specifically when the pending
    // badge disappears.
    const statusEls = await screen.findAllByRole("status");
    const badge = statusEls.find((el) => el.textContent.startsWith("Sending"));
    expect(badge).toBeDefined();

    // Locate the pending article. The row carries data-status="pending"
    // (set in Chat.tsx) and lives inside the message list.
    const list = screen.getByTestId("message-list");
    const pendingArticle = list.querySelector<HTMLElement>('article[data-status="pending"]');
    expect(pendingArticle).not.toBeNull();

    // AC-2: no inline `style` attribute, and no `opacity` in the inline
    // style declaration. Either condition catches a regression that
    // reintroduces `style={{ opacity: 0.6 }}`.
    const inlineStyle = pendingArticle?.getAttribute("style");
    expect(inlineStyle === null || inlineStyle === "").toBe(true);
    expect(pendingArticle?.style.opacity ?? "").toBe("");

    // AC-3: italic body. Read computed style via the imported stylesheet
    // (`.msg--pending .msg__body { font-style: italic }`).
    const body = pendingArticle?.querySelector<HTMLElement>(".msg__body");
    expect(body).not.toBeNull();
    if (body !== null && body !== undefined) {
      const computed = window.getComputedStyle(body);
      expect(computed.fontStyle).toBe("italic");
    }

    // Drain the held postMessage so the test cleanup sees a settled
    // promise. The pending entry stays pending — no WS echo is delivered
    // in this test — but the network call no longer dangles.
    for (const r of resolvers) r();
    styleEl.remove();
  });
});
