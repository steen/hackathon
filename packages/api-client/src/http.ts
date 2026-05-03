import { ApiError } from "./errors.js";
import type {
  AuthResponse,
  Channel,
  Envelope,
  ListMessagesOptions,
  Message,
  User,
  WSTicket,
} from "./types.js";

export type FetchLike = typeof fetch;

export interface HttpOptions {
  baseUrl: string;
  getToken?: () => string | null | undefined;
  fetch?: FetchLike;
}

export class HttpClient {
  private readonly baseUrl: string;
  private readonly getToken: () => string | null | undefined;
  private readonly fetchImpl: FetchLike;

  constructor(opts: HttpOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    this.getToken = opts.getToken ?? (() => null);
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis);
  }

  getBaseUrl(): string {
    return this.baseUrl;
  }

  async login(username: string, password: string): Promise<AuthResponse> {
    return this.request<AuthResponse>("POST", "/api/auth/login", {
      username,
      password,
    });
  }

  async register(username: string, password: string, inviteCode: string): Promise<AuthResponse> {
    return this.request<AuthResponse>("POST", "/api/auth/register", {
      username,
      password,
      invite_code: inviteCode,
    });
  }

  async me(): Promise<User> {
    const data = await this.request<{ user: User }>("GET", "/api/auth/me");
    return data.user;
  }

  async logout(): Promise<void> {
    await this.request<unknown>("POST", "/api/auth/logout");
  }

  async wsTicket(): Promise<WSTicket> {
    return this.request<WSTicket>("POST", "/api/auth/ws-ticket");
  }

  async listChannels(): Promise<Channel[]> {
    const data = await this.request<{ channels: Channel[] }>("GET", "/api/channels");
    return data.channels;
  }

  async createChannel(name: string): Promise<Channel> {
    return this.request<Channel>("POST", "/api/channels", { name });
  }

  async listMessages(channelId: string, opts: ListMessagesOptions = {}): Promise<Message[]> {
    const qs = new URLSearchParams();
    if (opts.before) qs.set("before", opts.before);
    if (opts.limit !== undefined && opts.limit > 0) {
      qs.set("limit", String(opts.limit));
    }
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    const path = `/api/channels/${encodeURIComponent(channelId)}/messages${suffix}`;
    const data = await this.request<{ messages: Message[] }>("GET", path);
    return data.messages;
  }

  async postMessage(channelId: string, body: string): Promise<Message> {
    const path = `/api/channels/${encodeURIComponent(channelId)}/messages`;
    return this.request<Message>("POST", path, { body });
  }

  // Public so callers can hit endpoints not (yet) wrapped with a typed
  // method. Kept thin: encodes/decodes the envelope, nothing else.
  async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {};
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    const token = this.getToken();
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
    const init: RequestInit = { method, headers };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }
    const res = await this.fetchImpl(this.baseUrl + path, init);
    return decodeEnvelope<T>(res);
  }
}

async function decodeEnvelope<T>(res: Response): Promise<T> {
  const text = await res.text();
  if (text.trim().length === 0) {
    if (res.status >= 400) {
      throw new ApiError(res.status, "unknown", res.statusText || "error");
    }
    return undefined as T;
  }
  let parsed: Envelope<T>;
  try {
    parsed = JSON.parse(text) as Envelope<T>;
  } catch {
    throw new ApiError(
      res.status,
      "decode_error",
      `invalid JSON envelope (status ${String(res.status)})`,
    );
  }
  if (!parsed.ok) {
    const err = parsed.error;
    throw new ApiError(res.status, err?.code ?? "unknown", err?.message ?? "error");
  }
  return parsed.data as T;
}
