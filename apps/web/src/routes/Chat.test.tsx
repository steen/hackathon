import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

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

async function renderWithChannel(): Promise<HTMLTextAreaElement> {
  happyPath();
  render(
    <AuthProvider>
      <Chat />
    </AuthProvider>,
  );
  const ta = await screen.findByLabelText<HTMLTextAreaElement>("message");
  await waitFor(() => {
    expect(ta).not.toBeDisabled();
  });
  return ta;
}

describe("test_web_composer_is_textarea_with_aria_label_message", () => {
  it("renders a <textarea> for the composer (multiline-capable)", async () => {
    const ta = await renderWithChannel();
    expect(ta.tagName).toBe("TEXTAREA");
  });
});

describe("test_web_composer_enter_submits_draft", () => {
  it("Enter without Shift submits the draft and clears the textarea", async () => {
    const ta = await renderWithChannel();
    postMessageMock.mockResolvedValue({
      id: "M-new",
      channel_id: "C1",
      sender_user_id: "U1",
      body: "hello",
      created_at: "2026-01-01T00:00:10Z",
    });

    fireEvent.change(ta, { target: { value: "hello" } });
    expect(ta.value).toBe("hello");

    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(postMessageMock).toHaveBeenCalledWith("C1", "hello");
    });
    expect(ta.value).toBe("");
  });
});

describe("test_web_composer_shift_enter_inserts_newline_does_not_submit", () => {
  it("Shift+Enter does not call postMessage and does not clear the draft", async () => {
    const ta = await renderWithChannel();
    fireEvent.change(ta, { target: { value: "line one" } });

    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter", shiftKey: true });
      await Promise.resolve();
    });

    expect(postMessageMock).not.toHaveBeenCalled();
    expect(ta.value).toBe("line one");
  });
});

describe("test_web_composer_enter_during_ime_composition_does_not_submit", () => {
  it("Enter that fires during composition (IME candidate commit) is ignored", async () => {
    const ta = await renderWithChannel();
    fireEvent.change(ta, { target: { value: "draft" } });

    await act(async () => {
      fireEvent.compositionStart(ta);
      fireEvent.keyDown(ta, { key: "Enter", isComposing: true });
      await Promise.resolve();
    });

    expect(postMessageMock).not.toHaveBeenCalled();
    expect(ta.value).toBe("draft");
  });
});

describe("test_web_composer_byte_counter_appears_at_warn_threshold", () => {
  it("counter is hidden well below cap, appears at >=80% of 4 KiB", async () => {
    const ta = await renderWithChannel();
    expect(screen.queryByTestId("composer-counter")).toBeNull();

    fireEvent.change(ta, { target: { value: "x".repeat(100) } });
    expect(screen.queryByTestId("composer-counter")).toBeNull();

    // 80% of 4096 = 3276.8 — 3277 chars (1 byte each in ASCII) should
    // cross the warn threshold.
    fireEvent.change(ta, { target: { value: "x".repeat(3277) } });
    const counter = await screen.findByTestId("composer-counter");
    expect(counter).toHaveClass("composer__counter--warn");
    expect(counter.textContent).toContain("3277");
    expect(counter.textContent).toContain("4096");
  });
});

describe("test_web_composer_over_cap_disables_send_and_shows_error_state", () => {
  it("over 4 KiB disables Send, shows error counter, blocks Enter submit", async () => {
    const ta = await renderWithChannel();
    fireEvent.change(ta, { target: { value: "x".repeat(4097) } });

    const counter = await screen.findByTestId("composer-counter");
    expect(counter).toHaveClass("composer__counter--error");
    expect(counter.textContent).toContain("too long to send");

    const sendBtn = screen.getByRole("button", { name: "Send" });
    expect(sendBtn).toBeDisabled();
    expect(ta).toHaveAttribute("aria-invalid", "true");

    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });
    expect(postMessageMock).not.toHaveBeenCalled();
  });
});

describe("test_web_composer_byte_counter_uses_utf8_byte_length_not_char_count", () => {
  it("multibyte chars count by encoded bytes (4 bytes per emoji)", async () => {
    const ta = await renderWithChannel();
    // 1000 four-byte rocket emojis = 4000 bytes (>3276 warn threshold,
    // <4096 cap). Rocket is U+1F680, encoded as 4 UTF-8 bytes.
    fireEvent.change(ta, { target: { value: "\u{1F680}".repeat(1000) } });
    const counter = await screen.findByTestId("composer-counter");
    expect(counter.textContent).toContain("4000");
    expect(counter).toHaveClass("composer__counter--warn");
  });
});

describe("test_web_composer_failed_message_badge_renders_on_post_failure", () => {
  it("post failure surfaces the failed badge with retry control", async () => {
    const ta = await renderWithChannel();
    postMessageMock.mockRejectedValue(new Error("boom"));

    fireEvent.change(ta, { target: { value: "doomed" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    const badge = await screen.findByTestId("msg-failed-badge");
    expect(badge.textContent).toBe("Failed to send");
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});
