import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { humanizeTimestamp } from "../utils/formatTimestamp.js";

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
import { Chat, IS_AT_BOTTOM_TOLERANCE_PX } from "./Chat.js";

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

describe("test_web_messages_list_has_aria_live_log_region", () => {
  it("messages list carries role=log (implicit aria-live=polite) so SR users hear new arrivals", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const list = await screen.findByTestId("message-list");
    expect(list).toHaveAttribute("role", "log");
    // No explicit aria-live: role="log" implies aria-live="polite" per
    // ARIA 1.2; one source of truth so the role and attribute can't
    // drift if a future change flips the announcement behavior.
    expect(list).not.toHaveAttribute("aria-live");
    expect(list).toHaveAttribute("aria-relevant", "additions");
    // aria-atomic="false" so SR announces only the newly added <article>,
    // not the full transcript every time a message arrives.
    expect(list).toHaveAttribute("aria-atomic", "false");
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

describe("test_web_presence_live_region_announces_join_with_known_username", () => {
  it("announces a join by username when the id is in the seeded directory", async () => {
    happyPath();
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({
          users: [
            { id: "U1", username: "alice" },
            { id: "U2", username: "bob" },
          ],
        });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const live = await screen.findByTestId("presence-live-region");
    // aria-live="polite" is sufficient on its own — the explicit role
    // is omitted to keep `getByRole("status")` (used by the e2e
    // suite to locate the connection badge) returning exactly one
    // element.
    expect(live).toHaveAttribute("aria-live", "polite");
    expect(live).toHaveAttribute("aria-atomic", "true");
    expect(live.textContent).toBe("");

    await waitFor(() => {
      expect(screen.getByTestId("presence-user-U2")).toBeInTheDocument();
    });
    const presenceSock = FakeSocket.instances.find((s) => !s.url.includes("channel="));
    expect(presenceSock).toBeDefined();

    // U2 leaves first so U2's later rejoin lands as a known username (the
    // seeded directory entry persists across leaves).
    await act(async () => {
      presenceSock?.open();
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "leave", user_id: "U2" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("bob left");
    });

    await act(async () => {
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U2" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("bob joined");
    });
  });
});

describe("test_web_presence_live_region_falls_back_when_id_unknown", () => {
  it("announces 'a new user joined' when the id is not in the seeded directory", async () => {
    happyPath();
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({ users: [] });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const live = await screen.findByTestId("presence-live-region");
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => !s.url.includes("channel="))).toBe(true);
    });
    const presenceSock = FakeSocket.instances.find((s) => !s.url.includes("channel="));

    await act(async () => {
      presenceSock?.open();
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U-stranger" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("a new user joined");
    });
  });

  it("announces 'a user left' when the leaving id is not in the seeded directory", async () => {
    happyPath();
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({ users: [] });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const live = await screen.findByTestId("presence-live-region");
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => !s.url.includes("channel="))).toBe(true);
    });
    const presenceSock = FakeSocket.instances.find((s) => !s.url.includes("channel="));

    await act(async () => {
      presenceSock?.open();
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "leave", user_id: "U-stranger" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("a user left");
    });
  });
});

describe("test_web_presence_live_region_rebroadcasts_join_when_already_present", () => {
  it("re-fires the live-region announcement on a repeat join for a user already in the list (seq-bumped state forces re-eval)", async () => {
    happyPath();
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({
          users: [
            { id: "U1", username: "alice" },
            { id: "U2", username: "bob" },
          ],
        });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const live = await screen.findByTestId("presence-live-region");
    await waitFor(() => {
      expect(screen.getByTestId("presence-user-U1")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByTestId("presence-user-U2")).toBeInTheDocument();
    });

    const presenceSock = FakeSocket.instances.find((s) => !s.url.includes("channel="));
    expect(presenceSock).toBeDefined();

    // First join for U1, who is already seeded into the list. Without the
    // `seq` field on PresenceEvent the lastEvent object would be referentially
    // equal to its predecessor (kind+id+username unchanged) and the
    // useMemo announcement would not re-evaluate.
    await act(async () => {
      presenceSock?.open();
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U1" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("alice joined");
    });

    // Intervening event flips the announcement to a different string so the
    // next U1 join is observable as a text *change*, not a no-op re-render.
    await act(async () => {
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "leave", user_id: "U2" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("bob left");
    });
    expect(screen.getByTestId("presence-user-U1")).toBeInTheDocument();

    // Second join for U1 while still in the list. The `seq` bump produces a
    // fresh lastEvent object, the useMemo re-fires, and the live region
    // flips back to "alice joined" — the path the existing tests miss.
    await act(async () => {
      presenceSock?.onmessage?.({
        data: JSON.stringify({
          type: "presence",
          data: { kind: "join", user_id: "U1" },
        }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(live.textContent).toBe("alice joined");
    });
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

describe("test_web_composer_over_cap_drops_aria_describedby_keeps_aria_errormessage", () => {
  it("warn state uses aria-describedby; over-cap drops describedby and keeps errormessage so SRs do not double-announce the same id", async () => {
    const ta = await renderWithChannel();
    const form = ta.closest("form");
    expect(form).not.toBeNull();
    if (form === null) return;

    // Below warn threshold: no pointer at the counter from either attribute.
    expect(form.getAttribute("aria-describedby")).toBeNull();
    expect(ta.getAttribute("aria-errormessage")).toBeNull();

    // Warn band (>=80%, <=cap): describedby points at the counter; no errormessage yet.
    fireEvent.change(ta, { target: { value: "x".repeat(3277) } });
    await screen.findByTestId("composer-counter");
    expect(form.getAttribute("aria-describedby")).toBe("composer-counter");
    expect(ta.getAttribute("aria-errormessage")).toBeNull();

    // Over-cap: only errormessage points at the counter; describedby is dropped
    // so SR does not announce the same element via two relations.
    fireEvent.change(ta, { target: { value: "x".repeat(4097) } });
    expect(ta.getAttribute("aria-errormessage")).toBe("composer-counter");
    expect(form.getAttribute("aria-describedby")).toBeNull();
  });
});

describe("test_web_composer_over_cap_counter_stays_role_status_not_alert", () => {
  it("counter keeps role=status (polite) once over-cap so each keystroke past the cap is not re-announced as a fresh alert", async () => {
    const ta = await renderWithChannel();
    fireEvent.change(ta, { target: { value: "x".repeat(4097) } });

    const counter = await screen.findByTestId("composer-counter");
    expect(counter).toHaveAttribute("role", "status");
    expect(counter).not.toHaveAttribute("role", "alert");

    // Mutating the draft past the cap (additional keystrokes) must not flip
    // the role to alert — the error mode is already conveyed by the
    // textarea's aria-errormessage and the visible --error class.
    fireEvent.change(ta, { target: { value: "x".repeat(4200) } });
    expect(counter).toHaveAttribute("role", "status");
    expect(counter).toHaveClass("composer__counter--error");
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

describe("test_web_failed_message_badge_uses_role_alert", () => {
  it("the failed-send badge announces assertively (role=alert), not politely", async () => {
    const ta = await renderWithChannel();
    postMessageMock.mockRejectedValue(new Error("boom"));

    fireEvent.change(ta, { target: { value: "doomed" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    // role="alert" implies aria-live="assertive" — the right level for an
    // actionable, time-sensitive send failure. role="status" (the previous
    // value) implies polite, which buries the announcement behind whatever
    // the SR is already speaking.
    const badge = await screen.findByTestId("msg-failed-badge");
    expect(badge).toHaveAttribute("role", "alert");
    expect(badge).not.toHaveAttribute("role", "status");
  });
});

describe("test_web_failed_message_badge_surfaces_curated_failure_reason", () => {
  it("the failed badge points at a sibling reason via aria-describedby", async () => {
    const ta = await renderWithChannel();
    // Plain Error → REASON_GENERIC ("Something went wrong.") via classifyError.
    postMessageMock.mockRejectedValue(new Error("boom"));

    fireEvent.change(ta, { target: { value: "doomed" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    const badge = await screen.findByTestId("msg-failed-badge");
    const describedBy = badge.getAttribute("aria-describedby");
    expect(describedBy).not.toBeNull();
    expect(describedBy ?? "").toMatch(/^msg-failed-reason-/);

    const reasonEl = describedBy === null ? null : document.getElementById(describedBy);
    expect(reasonEl).not.toBeNull();
    expect(reasonEl?.textContent).toBe("Something went wrong.");
    // The raw err.message must not leak into the visible reason.
    expect(reasonEl?.textContent ?? "").not.toContain("boom");
  });
});

describe("test_web_pending_message_renders_sending_badge_italic_no_opacity", () => {
  it("posts a message and asserts the pending row carries the badge, italic body, and no inline style", async () => {
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

    // AC-1: the Sending… badge renders with text starting "Sending".
    // findByText handles a missing match by throwing, so the lookup
    // doubles as the assertion.
    await screen.findByText(/^Sending/);

    // Locate the pending article. The row carries data-status="pending"
    // (set in Chat.tsx) and lives inside the message list.
    const list = screen.getByTestId("message-list");
    const pendingArticle = list.querySelector<HTMLElement>('article[data-status="pending"]');
    expect(pendingArticle).not.toBeNull();

    // AC-2: no inline `style` attribute. Catches a regression that
    // reintroduces `style={{ opacity: 0.6 }}`.
    const inlineStyle = pendingArticle?.getAttribute("style");
    expect(inlineStyle === null || inlineStyle === "").toBe(true);

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
  });
});

describe("test_web_self_authored_optimistic_message_is_aria_hidden_for_sr", () => {
  it("self-authored optimistic-send <article> carries aria-hidden=true while data-status=pending so the polite log region does not read the user's own message back to them", async () => {
    happyPath();
    // Hold postMessage open so the optimistic entry stays pending across
    // the assertions. Resolvers drained after the test prevents leaked
    // promises.
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

    const ta = await screen.findByLabelText<HTMLTextAreaElement>("message");
    await waitFor(() => {
      expect(ta).not.toBeDisabled();
    });

    fireEvent.change(ta, { target: { value: "my own message" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(postMessageMock).toHaveBeenCalledWith("C1", "my own message");
    });

    const list = screen.getByTestId("message-list");
    const pendingArticle = await waitFor(() => {
      const a = list.querySelector<HTMLElement>('article[data-status="pending"]');
      expect(a).not.toBeNull();
      return a;
    });
    expect(pendingArticle?.getAttribute("aria-hidden")).toBe("true");

    for (const r of resolvers) r();
  });
});

describe("test_web_self_authored_aria_hidden_persists_after_ws_reconcile", () => {
  it("self-authored row stays aria-hidden=true after the WS echo lands and data-status flips pending→sent", async () => {
    happyPath();
    // Force the WS-echo reconcile path (not the REST-response shortcut):
    // returning undefined from postMessage skips the in-place swap in
    // submitPending, so the pending row only flips to sent when the
    // mocked WS frame arrives. This is the path #575 is regression-
    // covering — a bug minting a new row id on echo would re-introduce
    // SR readback for self-authored sent rows.
    postMessageMock.mockResolvedValue(undefined);

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const ta = await screen.findByLabelText<HTMLTextAreaElement>("message");
    await waitFor(() => {
      expect(ta).not.toBeDisabled();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));
    expect(sock).toBeDefined();
    await act(async () => {
      sock?.open();
      await Promise.resolve();
    });

    fireEvent.change(ta, { target: { value: "my own message" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(postMessageMock).toHaveBeenCalledWith("C1", "my own message");
    });

    const list = screen.getByTestId("message-list");
    const pendingArticle = await waitFor(() => {
      const a = list.querySelector<HTMLElement>('article[data-status="pending"]');
      expect(a).not.toBeNull();
      return a;
    });
    // Baseline: pending row is aria-hidden (covered by
    // test_web_self_authored_optimistic_message_is_aria_hidden_for_sr,
    // re-asserted here so the post-echo delta is unambiguous).
    expect(pendingArticle?.getAttribute("aria-hidden")).toBe("true");

    // Server echo for the same draft. happyPath() seeds the user as U1,
    // so sender_user_id matches self and useMessages folds this onto
    // the pending row by body+sender (the WS-side reconcile branch).
    await act(async () => {
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M-echo",
            channel_id: "C1",
            sender_user_id: "U1",
            body: "my own message",
            created_at: new Date().toISOString(),
          },
        }),
      });
      await Promise.resolve();
    });

    // After reconcile: status flipped pending→sent (data-status="sent"
    // is the default branch in Chat.tsx) and the row count is unchanged
    // (no duplicate appended).
    await waitFor(() => {
      const stillPending = list.querySelector('article[data-status="pending"]');
      expect(stillPending).toBeNull();
    });
    const articles = list.querySelectorAll<HTMLElement>('article[data-testid="msg"]');
    // History (M1, U2) + reconciled self row (M-echo, U1) = 2 articles.
    expect(articles).toHaveLength(2);

    // The reconciled self-authored row must still be aria-hidden so SR
    // users do not hear their own outbound message read back when the
    // server echo lands. Locate by body text to disambiguate from the
    // history row (also data-status="sent").
    const selfArticle = Array.from(articles).find(
      (a) => a.querySelector(".msg__body")?.textContent === "my own message",
    );
    expect(selfArticle).toBeDefined();
    expect(selfArticle?.getAttribute("aria-hidden")).toBe("true");
    // History row (sender U2) must remain announceable — belt-and-
    // braces against a regression that flips the predicate the wrong way.
    const otherArticle = Array.from(articles).find(
      (a) => a.querySelector(".msg__body")?.textContent === "hello from history",
    );
    expect(otherArticle).toBeDefined();
    expect(otherArticle?.getAttribute("aria-hidden")).toBeNull();
  });
});

describe("test_web_other_authored_message_is_not_aria_hidden", () => {
  it("a message whose sender is another user is not aria-hidden — SR users still hear inbound traffic", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    // History fixture in happyPath includes M1 from sender U2 (not self U1).
    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });

    const list = screen.getByTestId("message-list");
    const articles = list.querySelectorAll<HTMLElement>('article[data-testid="msg"]');
    expect(articles.length).toBeGreaterThan(0);
    for (const a of articles) {
      // History row is from U2, so no article should be aria-hidden here —
      // belt-and-suspenders against a future regression that aria-hides
      // the wrong rows.
      expect(a.getAttribute("aria-hidden")).toBeNull();
    }
  });
});

describe("humanizeTimestamp", () => {
  // Build local-zone ISO so the helper's local-day comparison is
  // deterministic regardless of where the test runs.
  function localIso(y: number, mo: number, d: number, h: number, mi: number): string {
    const pad = (n: number): string => (n < 10 ? `0${String(n)}` : String(n));
    return `${String(y)}-${pad(mo)}-${pad(d)}T${pad(h)}:${pad(mi)}:00`;
  }

  it("returns empty string for empty input", () => {
    expect(humanizeTimestamp("")).toBe("");
  });

  it("returns the raw input when it is not a parseable date", () => {
    expect(humanizeTimestamp("not-a-date")).toBe("not-a-date");
  });

  it("today renders as HH:MM (24h)", () => {
    const now = new Date(2026, 4, 4, 10, 0, 0);
    const iso = localIso(2026, 5, 4, 14, 32);
    expect(humanizeTimestamp(iso, now)).toBe("14:32");
  });

  it("yesterday renders as 'Wkd HH:MM'", () => {
    const now = new Date(2026, 4, 4, 10, 0, 0); // Mon May 4 2026
    const iso = localIso(2026, 5, 3, 23, 50); // Sun May 3
    // Date(2026,4,3) is a Sunday — Intl en-US short weekday is "Sun".
    expect(humanizeTimestamp(iso, now)).toBe("Sun 23:50");
  });

  it("six days ago still renders as 'Wkd HH:MM'", () => {
    const now = new Date(2026, 4, 8, 9, 0, 0); // Fri May 8
    const iso = localIso(2026, 5, 2, 8, 5); // Sat May 2 → 6 days ago
    expect(humanizeTimestamp(iso, now)).toBe("Sat 08:05");
  });

  it("seven+ days ago renders as 'Mon D HH:MM'", () => {
    const now = new Date(2026, 4, 4, 10, 0, 0);
    const iso = localIso(2026, 1, 1, 0, 0);
    expect(humanizeTimestamp(iso, now)).toBe("Jan 1 00:00");
  });

  it("crosses midnight by local-day, not 24h window", () => {
    // 23:50 yesterday viewed at 00:10 today is < 24h apart, but a
    // different local day → must take the "Wkd HH:MM" branch, not the
    // today branch and not the older "Mon D HH:MM" branch.
    const now = new Date(2026, 4, 4, 0, 10, 0);
    const iso = localIso(2026, 5, 3, 23, 50);
    expect(humanizeTimestamp(iso, now)).toBe("Sun 23:50");
  });
});

describe("test_web_message_timestamp_renders_humanized_form_not_raw_iso", () => {
  it("recent message label is the humanized form; <time dateTime> keeps the ISO", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    const recentIso = new Date(Date.now() - 60_000).toISOString();
    listMessagesMock.mockResolvedValue([
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "recent body",
        created_at: recentIso,
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
      expect(screen.getByText("recent body")).toBeInTheDocument();
    });

    const list = screen.getByTestId("message-list");
    const timeEl = list.querySelector("time");
    expect(timeEl).not.toBeNull();
    expect(timeEl?.getAttribute("datetime")).toBe(recentIso);
    // Visible label is no longer the raw ISO — it must not contain the
    // 'T' separator nor the 'Z' suffix that mark RFC3339.
    expect(timeEl?.textContent ?? "").not.toBe(recentIso);
    expect(timeEl?.textContent ?? "").not.toContain("T");
    expect(timeEl?.textContent ?? "").not.toMatch(/Z$/);
    // For a one-minute-old message the format is HH:MM (today branch).
    expect(timeEl?.textContent ?? "").toMatch(/^\d{2}:\d{2}$/);
  });
});

describe("test_web_message_sender_renders_username_when_known", () => {
  it("a sender id present in the /api/presence seed renders as the username, not the UUID", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    listMessagesMock.mockResolvedValue([
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "hello from bob",
        created_at: "2026-01-01T00:00:00Z",
      },
    ]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({
          users: [
            { id: "U1", username: "alice" },
            { id: "U2", username: "bob" },
          ],
        });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const list = await screen.findByTestId("message-list");
    await waitFor(() => {
      const sender = list.querySelector(".msg__sender");
      expect(sender?.textContent).toBe("bob");
    });
    // Belt-and-suspenders: the UUID itself must not appear in the sender slot.
    const sender = list.querySelector(".msg__sender");
    expect(sender?.textContent).not.toBe("U2");
  });
});

describe("test_web_message_sender_falls_back_to_uuid_when_unknown", () => {
  it("a sender id absent from the directory renders as the raw id (no crash)", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    listMessagesMock.mockResolvedValue([
      {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U-stranger",
        body: "hello from a stranger",
        created_at: "2026-01-01T00:00:00Z",
      },
    ]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    // Empty seed — U-stranger never enters the directory.
    httpRequestMock.mockImplementation((method: string, path: string) => {
      if (method === "GET" && path === "/api/presence") {
        return Promise.resolve({ users: [] });
      }
      return Promise.reject(new Error(`unexpected http.request: ${method} ${path}`));
    });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const list = await screen.findByTestId("message-list");
    await waitFor(() => {
      expect(screen.getByText("hello from a stranger")).toBeInTheDocument();
    });
    const sender = list.querySelector(".msg__sender");
    expect(sender?.textContent).toBe("U-stranger");
  });
});

describe("test_web_chat_focus_management_mount_no_channels_focuses_heading", () => {
  it("focuses the channel-name heading on mount when no channels exist (composer disabled)", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([]);
    listMessagesMock.mockResolvedValue([]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const heading = await screen.findByRole("heading", { name: /select a channel/i });
    await waitFor(() => {
      expect(document.activeElement).toBe(heading);
    });
    expect(heading.getAttribute("tabindex")).toBe("-1");
  });
});

describe("test_web_chat_focus_management_mount_with_channel_focuses_composer", () => {
  it("focuses the composer once a channel becomes active (composer branch wins over heading)", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const composer = await screen.findByLabelText<HTMLTextAreaElement>("message");
    await waitFor(() => {
      expect(composer).not.toBeDisabled();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(composer);
    });
  });
});

describe("test_web_chat_focus_promotes_to_composer_when_channels_resolve_after_paint", () => {
  it("first paint focuses the heading (channels still loading), then promotes focus to the composer once useChannels resolves", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    // Hold listChannels open across the initial paint so the channel list
    // is empty long enough for the heading branch to win first; then
    // resolve and assert the focus effect re-runs and lands on the composer.
    let resolveChannels: ((rows: unknown[]) => void) | undefined;
    listChannelsMock.mockImplementation(
      () =>
        new Promise<unknown[]>((resolve) => {
          resolveChannels = resolve;
        }),
    );
    listMessagesMock.mockResolvedValue([]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const heading = await screen.findByRole("heading", { name: /select a channel/i });
    await waitFor(() => {
      expect(document.activeElement).toBe(heading);
    });

    await act(async () => {
      resolveChannels?.([{ id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" }]);
      await Promise.resolve();
    });

    const composer = await screen.findByLabelText<HTMLTextAreaElement>("message");
    await waitFor(() => {
      expect(composer).not.toBeDisabled();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(composer);
    });
  });
});

describe("test_web_chat_focus_does_not_steal_back_to_composer_after_user_moves_away", () => {
  it("does not pull focus back to the composer when state changes after the user has tabbed elsewhere", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const composer = await screen.findByLabelText<HTMLTextAreaElement>("message");
    await waitFor(() => {
      expect(document.activeElement).toBe(composer);
    });

    // Simulate the user moving focus to the Sign out button.
    const signOut = screen.getByRole("button", { name: /sign out/i });
    act(() => {
      signOut.focus();
    });
    expect(document.activeElement).toBe(signOut);

    // A subsequent state change (typing in the composer) updates Chat
    // state. The focus effect must not re-run and steal focus back.
    act(() => {
      fireEvent.change(composer, { target: { value: "hi" } });
    });
    expect(document.activeElement).toBe(signOut);
  });
});

describe("test_web_chat_landmarks_have_accessible_names", () => {
  it("aside, main, and sidebar lists each expose an accessible name", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });

    // <aside> resolves to role="complementary" with the explicit aria-label.
    expect(screen.getByRole("complementary", { name: "Chat sidebar" })).toBeInTheDocument();

    // <main> takes the active channel name as its label so SR landmark
    // navigation announces "general, main" rather than an unnamed region.
    expect(screen.getByRole("main", { name: "general" })).toBeInTheDocument();

    // Each sidebar <ul> is named so SR landmark / list navigation has a
    // label distinct from the heading text alone.
    expect(screen.getByRole("list", { name: "Channels" })).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Online users" })).toBeInTheDocument();
  });

  it("main landmark falls back to 'Messages' when no channel is active", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([]);
    listMessagesMock.mockResolvedValue([]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    expect(await screen.findByRole("main", { name: "Messages" })).toBeInTheDocument();
  });
});

describe("test_web_chat_empty_channels_renders_wait_for_admin_copy", () => {
  it("zero-channel state renders the 'wait for an admin' empty-state inside the messages log region", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([]);
    listMessagesMock.mockResolvedValue([]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const empty = await screen.findByTestId("empty-state-no-channels");
    expect(empty.textContent).toBe("No channels available yet. Wait for an admin to create one.");
    // Sits inside the role="log" region so the polite-live announcement
    // path covers SR users; no separate live region needed.
    const list = screen.getByTestId("message-list");
    expect(list.contains(empty)).toBe(true);
    // Composer stays disabled with no channel selected.
    const ta = await screen.findByLabelText<HTMLTextAreaElement>("message");
    expect(ta).toBeDisabled();
  });
});

describe("test_web_chat_load_older_button_visible_only_after_full_page", () => {
  it("does not render the Load older button when the initial history page is short", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("load-older-button")).toBeNull();
  });

  it("renders the Load older button when the initial page is full and clicking it requests before=<oldest>", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    // A full 50-row newest-first page. After the hook reverses it, M001
    // is the top-of-list (oldest) row — the ULID that loadOlder must
    // forward as `before`.
    const initial = Array.from({ length: 50 }, (_, i) => ({
      id: `M${String(50 - i).padStart(3, "0")}`,
      channel_id: "C1",
      sender_user_id: "U2",
      body: `body-${String(50 - i)}`,
      created_at: "2026-01-01T00:00:00Z",
    }));
    const older = Array.from({ length: 1 }, () => ({
      id: "M000",
      channel_id: "C1",
      sender_user_id: "U2",
      body: "older",
      created_at: "2025-12-31T00:00:00Z",
    }));
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockResolvedValueOnce(older);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const trigger = await screen.findByTestId("load-older-button");
    expect(trigger.tagName).toBe("BUTTON");
    expect(trigger.textContent).toBe("Load older messages");

    await act(async () => {
      fireEvent.click(trigger);
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(listMessagesMock).toHaveBeenCalledWith("C1", { before: "M001", limit: 50 });
    });

    // Older page returned a single row — short page → trigger hides.
    await waitFor(() => {
      expect(screen.queryByTestId("load-older-button")).toBeNull();
    });
    // The prepended row sits above the previous-top row.
    const list = screen.getByTestId("message-list");
    const bodies = Array.from(list.querySelectorAll<HTMLElement>(".msg__body")).map(
      (el) => el.textContent,
    );
    expect(bodies[0]).toBe("older");
    expect(bodies[1]).toBe("body-1");
  });
});

describe("test_web_chat_load_older_button_loading_affordance", () => {
  it("flips disabled + aria-busy + label to 'Loading older messages…' while a fetch is in flight", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    const initial = Array.from({ length: 50 }, (_, i) => ({
      id: `M${String(50 - i).padStart(3, "0")}`,
      channel_id: "C1",
      sender_user_id: "U2",
      body: `body-${String(50 - i)}`,
      created_at: "2026-01-01T00:00:00Z",
    }));
    listMessagesMock.mockResolvedValueOnce(initial);
    let resolveOlder: ((rows: unknown[]) => void) | undefined;
    listMessagesMock.mockImplementationOnce(
      () =>
        new Promise<unknown[]>((resolve) => {
          resolveOlder = resolve;
        }),
    );
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const trigger = await screen.findByTestId("load-older-button");
    expect(trigger).not.toBeDisabled();
    expect(trigger).not.toHaveAttribute("aria-busy");
    expect(trigger.textContent).toBe("Load older messages");

    await act(async () => {
      fireEvent.click(trigger);
      await Promise.resolve();
    });

    // Mid-fetch: button is disabled, announces busy, and the label flips.
    expect(trigger).toBeDisabled();
    expect(trigger).toHaveAttribute("aria-busy", "true");
    expect(trigger.textContent).toBe("Loading older messages…");

    // A second click while busy must not fire a second request — the
    // disabled flip prevents the click; loadingOlderRef belt-and-braces it.
    await act(async () => {
      fireEvent.click(trigger);
      await Promise.resolve();
    });
    expect(listMessagesMock).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveOlder?.([
        {
          id: "M000",
          channel_id: "C1",
          sender_user_id: "U2",
          body: "older",
          created_at: "2025-12-31T00:00:00Z",
        },
      ]);
      await Promise.resolve();
    });

    // Settled: short older page → trigger hides entirely.
    await waitFor(() => {
      expect(screen.queryByTestId("load-older-button")).toBeNull();
    });
  });
});

describe("test_web_chat_load_older_failure_renders_inline_not_in_channel_banner", () => {
  it("loadOlder failure renders inline below the trigger and does not mount the channel-level alert", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    const initial = Array.from({ length: 50 }, (_, i) => ({
      id: `M${String(50 - i).padStart(3, "0")}`,
      channel_id: "C1",
      sender_user_id: "U2",
      body: `body-${String(50 - i)}`,
      created_at: "2026-01-01T00:00:00Z",
    }));
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockRejectedValueOnce(new Error("network down"));
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const trigger = await screen.findByTestId("load-older-button");

    await act(async () => {
      fireEvent.click(trigger);
      await Promise.resolve();
    });

    // Inline error sits next to the trigger, role=alert so SR users hear it.
    const inline = await screen.findByTestId("load-older-error");
    expect(inline).toHaveAttribute("role", "alert");
    expect(inline.textContent).toBe("Failed to load older messages: Something went wrong.");
    // Raw err.message must not leak into the visible copy.
    expect(inline.textContent).not.toContain("network down");

    // Channel-level <p role="alert" class="error"> for history/WS faults
    // must not have mounted — the per-trigger error is on its own slot.
    // The inline element shares the `error` class but carries the
    // distinguishing `messages__load-older-error` class.
    const list = screen.getByTestId("message-list");
    const channelBanners = Array.from(list.querySelectorAll<HTMLElement>("p.error")).filter(
      (el) => !el.classList.contains("messages__load-older-error"),
    );
    expect(channelBanners).toHaveLength(0);

    // Trigger remains visible (canLoadOlder did not flip) so the user
    // can retry.
    expect(screen.getByTestId("load-older-button")).toBeInTheDocument();
    consoleErrorSpy.mockRestore();
  });
});

describe("test_web_chat_empty_messages_in_selected_channel_renders_start_of_channel_hint", () => {
  it("populated channels with an empty messages list renders the 'start of #general' hint", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    listMessagesMock.mockResolvedValue([]);
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    const hint = await screen.findByTestId("empty-state-channel-hint");
    expect(hint.textContent).toBe("This is the start of #general — send a message to say hi.");
    // No-channels copy must not render once a channel is active.
    expect(screen.queryByTestId("empty-state-no-channels")).toBeNull();
  });
});

describe("test_web_chat_does_not_flash_empty_channel_hint_before_history_resolves", () => {
  it("populated channel does not render the start-of-channel hint while listMessages is in flight", async () => {
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" },
    ]);
    // Hold listMessages open until after we have asserted that the hint
    // is absent. The state at this point — connecting WS, empty messages,
    // null error — is the exact race the historyLoading gate covers.
    let resolveHistory: ((rows: unknown[]) => void) | undefined;
    listMessagesMock.mockImplementation(
      () =>
        new Promise<unknown[]>((resolve) => {
          resolveHistory = resolve;
        }),
    );
    wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
    httpRequestMock.mockResolvedValue({ users: [] });

    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    // The channel heading reflecting the active channel is the earliest
    // signal that activeChannel has been set; from this point onward the
    // race window in Chat.tsx (connecting + empty messages + null error)
    // is open until listMessages settles.
    await screen.findByRole("heading", { name: /^general$/ });
    expect(screen.queryByTestId("empty-state-channel-hint")).toBeNull();

    await act(async () => {
      resolveHistory?.([
        {
          id: "M1",
          channel_id: "C1",
          sender_user_id: "U2",
          body: "hello",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]);
      await Promise.resolve();
    });

    // Once history lands with rows, the hint stays absent (messages>0)
    // and at least one article renders.
    const articles = await screen.findAllByRole("article");
    expect(articles.length).toBeGreaterThan(0);
    expect(screen.queryByTestId("empty-state-channel-hint")).toBeNull();
  });
});

describe("test_web_message_list_respects_user_scroll_when_live_message_arrives", () => {
  it("does not auto-scroll to bottom when the user has scrolled up", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));
    expect(sock).toBeDefined();

    const list = screen.getByTestId("message-list");
    // jsdom doesn't lay out, so scrollHeight/clientHeight are 0 and the
    // is-at-bottom check would always be true. Stub the geometry to model
    // a viewport that's been scrolled well above the bottom (distance =
    // scrollHeight - (scrollTop + clientHeight) = 500, far past the 8px
    // tolerance). scrollTop is writable in jsdom; the others are getter
    // properties redefined here.
    Object.defineProperty(list, "scrollHeight", { value: 1000, configurable: true });
    Object.defineProperty(list, "clientHeight", { value: 400, configurable: true });
    list.scrollTop = 100;
    fireEvent.scroll(list);

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M2",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live arrival while reading history",
            created_at: "2026-01-01T00:00:01Z",
          },
        }),
      });
      await Promise.resolve();
    });

    expect(screen.getByText("live arrival while reading history")).toBeInTheDocument();
    // The auto-scroll effect must not have fired — scrollTop stays at the
    // user's position, not jumped to scrollHeight (1000).
    expect(list.scrollTop).toBe(100);
  });

  it("auto-scrolls to bottom when the user is pinned at the bottom", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));

    const list = screen.getByTestId("message-list");
    // Geometry: user is pinned at bottom (distance = 0 ≤ 8px tolerance).
    Object.defineProperty(list, "scrollHeight", { value: 1000, configurable: true });
    Object.defineProperty(list, "clientHeight", { value: 400, configurable: true });
    list.scrollTop = 600;
    fireEvent.scroll(list);

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M2",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live arrival at bottom",
            created_at: "2026-01-01T00:00:01Z",
          },
        }),
      });
      await Promise.resolve();
    });

    expect(screen.getByText("live arrival at bottom")).toBeInTheDocument();
    // The effect ran and pinned scrollTop to scrollHeight.
    expect(list.scrollTop).toBe(1000);
  });

  it("auto-scrolls when distance from bottom is exactly 8px (boundary inclusive)", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));

    const list = screen.getByTestId("message-list");
    // Boundary case: distance = scrollHeight - (scrollTop + clientHeight)
    // = IS_AT_BOTTOM_TOLERANCE_PX. The `<=` check in Chat.tsx must treat
    // this as still-at-bottom and fire the auto-scroll. If a refactor
    // tightens the comparison to `<`, this case regresses.
    const scrollHeight = 1000;
    const clientHeight = 400;
    Object.defineProperty(list, "scrollHeight", { value: scrollHeight, configurable: true });
    Object.defineProperty(list, "clientHeight", { value: clientHeight, configurable: true });
    list.scrollTop = scrollHeight - clientHeight - IS_AT_BOTTOM_TOLERANCE_PX;
    fireEvent.scroll(list);

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M2",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live arrival at 8px boundary",
            created_at: "2026-01-01T00:00:01Z",
          },
        }),
      });
      await Promise.resolve();
    });

    expect(screen.getByText("live arrival at 8px boundary")).toBeInTheDocument();
    expect(list.scrollTop).toBe(scrollHeight);
  });

  it("does not auto-scroll when distance from bottom is 9px (boundary exclusive)", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("hello from history")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(FakeSocket.instances.some((s) => s.url.includes("channel=C1"))).toBe(true);
    });
    const sock = FakeSocket.instances.find((s) => s.url.includes("channel=C1"));

    const list = screen.getByTestId("message-list");
    // Boundary case: distance = IS_AT_BOTTOM_TOLERANCE_PX + 1, just past
    // the tolerance. The user counts as scrolled-up; auto-scroll must
    // not fire. If a refactor loosens the comparison, this case regresses.
    const scrollHeight = 1000;
    const clientHeight = 400;
    const scrollTop = scrollHeight - clientHeight - (IS_AT_BOTTOM_TOLERANCE_PX + 1);
    Object.defineProperty(list, "scrollHeight", { value: scrollHeight, configurable: true });
    Object.defineProperty(list, "clientHeight", { value: clientHeight, configurable: true });
    list.scrollTop = scrollTop;
    fireEvent.scroll(list);

    await act(async () => {
      sock?.open();
      sock?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: {
            id: "M2",
            channel_id: "C1",
            sender_user_id: "U3",
            body: "live arrival at 9px past boundary",
            created_at: "2026-01-01T00:00:01Z",
          },
        }),
      });
      await Promise.resolve();
    });

    expect(screen.getByText("live arrival at 9px past boundary")).toBeInTheDocument();
    expect(list.scrollTop).toBe(scrollTop);
  });
});

describe("test_web_conn_badge_phone_header_wraps_to_two_rows", () => {
  it("renders the connection badge inside the messages header", async () => {
    happyPath();
    render(
      <AuthProvider>
        <Chat />
      </AuthProvider>,
    );

    // The badge is the only role=status node in the messages region (the
    // presence live region is in the sidebar; comment at line ~462
    // documents this). It must always render so SR users can hear WS
    // state changes regardless of viewport width.
    const badge = await screen.findByRole("status");
    expect(badge).toBeInTheDocument();
    expect(badge.classList.contains("conn")).toBe(true);

    const header = badge.closest(".messages__header");
    expect(header).not.toBeNull();
    // h2 is rendered as a sibling of the badge inside the same header.
    expect(header?.querySelector("h2")).not.toBeNull();
  });

  // jsdom does not run layout, so getBoundingClientRect width assertions
  // would all return zero and silently pass. The contract that prevents
  // mobile clipping lives in the stylesheet itself: at ≤767px the header
  // must wrap, and the title must claim the full row so the stable-width
  // badge falls to row 2. Walk the injected stylesheet (test-setup.ts
  // attaches styles.css verbatim) and assert the rules exist with the
  // expected declarations. A future refactor that drops these rules
  // re-introduces the bug from #632 and fails this test.
  it("styles.css carries phone-width header wrap rules so the badge can never sit beside an overflowing title", () => {
    interface DecMatch {
      selector: string;
      declarations: Record<string, string>;
    }

    function collectPhoneRules(): DecMatch[] {
      const out: DecMatch[] = [];
      for (const sheet of Array.from(document.styleSheets)) {
        let rules: CSSRuleList;
        try {
          rules = sheet.cssRules;
        } catch {
          continue;
        }
        for (const rule of Array.from(rules)) {
          if (!(rule instanceof CSSMediaRule)) continue;
          // The baseline phone breakpoint is `(max-width: 767px)`. Match
          // exact text after whitespace normalization to avoid picking
          // up unrelated future media blocks.
          const condition = rule.conditionText.replace(/\s+/g, " ").trim();
          if (condition !== "(max-width: 767px)") continue;
          for (const inner of Array.from(rule.cssRules)) {
            if (!(inner instanceof CSSStyleRule)) continue;
            const decls: Record<string, string> = {};
            // jsdom's CSSStyleDeclaration exposes properties via numeric
            // index (e.g. `style[0]`) but does not implement `.item(i)`,
            // unlike the browser CSSOM. Spread to an array so we can
            // iterate via for-of without poking the indexed-access shape.
            const style = inner.style;
            const propNames = Array.from(
              { length: style.length },
              (_, i) => (style as unknown as Record<number, string>)[i],
            );
            for (const prop of propNames) {
              if (typeof prop !== "string" || prop.length === 0) continue;
              decls[prop] = style.getPropertyValue(prop).trim();
            }
            out.push({ selector: inner.selectorText, declarations: decls });
          }
        }
      }
      return out;
    }

    const rules = collectPhoneRules();
    const headerRule = rules.find((r) => r.selector === ".messages__header");
    expect(headerRule, "phone-width .messages__header rule").toBeDefined();
    expect(headerRule?.declarations["flex-wrap"]).toBe("wrap");

    const headerH2Rule = rules.find((r) => r.selector === ".messages__header h2");
    expect(headerH2Rule, "phone-width .messages__header h2 rule").toBeDefined();
    // jsdom's CSSOM does not expand the `flex` shorthand into the
    // -grow/-shrink/-basis longhands (Chrome does), so assert against
    // the shorthand text instead. The 100% basis is what forces the
    // badge to row 2 — without it the badge would sit beside a
    // shrunken title at narrow widths and the bug from #632 returns.
    expect(headerH2Rule?.declarations.flex).toContain("100%");
    // min-width: 0 is required to let the heading shrink in its flex
    // container; without it a long unbroken title would still overflow.
    // CSSOM serialization across jsdom versions varies (`0` vs `0px`).
    expect(["0", "0px"]).toContain(headerH2Rule?.declarations["min-width"]);
  });
});
