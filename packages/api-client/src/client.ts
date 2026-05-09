import { HttpClient, type FetchLike } from "./http.js";
import { WebSocketClient, watch as watchGen, type WebSocketCtor } from "./ws.js";
import type {
  AuthResponse,
  Channel,
  Event as WsEvent,
  ListMessagesOptions,
  Message,
  User,
  WSTicket,
} from "./types.js";

export interface ClientOptions {
  baseUrl: string;
  getToken?: () => string | null | undefined;
  setToken?: (token: string | null) => void;
  fetch?: FetchLike;
  WebSocket?: WebSocketCtor;
}

export class Client {
  readonly http: HttpClient;
  private readonly setTokenFn: (token: string | null) => void;
  private readonly wsCtor?: WebSocketCtor;
  private memToken: string | null = null;

  constructor(opts: ClientOptions) {
    this.memToken = null;
    const getToken = opts.getToken ?? ((): string | null => this.memToken);
    this.setTokenFn =
      opts.setToken ??
      ((tok): void => {
        this.memToken = tok;
      });
    this.http = new HttpClient({
      baseUrl: opts.baseUrl,
      getToken,
      fetch: opts.fetch,
    });
    this.wsCtor = opts.WebSocket;
  }

  async login(username: string, password: string): Promise<AuthResponse> {
    const out = await this.http.login(username, password);
    this.setTokenFn(out.token);
    return out;
  }

  async register(
    username: string,
    password: string,
    inviteCode: string,
    identity?: { boxPubkey: string; signPubkey: string },
  ): Promise<AuthResponse> {
    const out = await this.http.register(username, password, inviteCode, identity);
    this.setTokenFn(out.token);
    return out;
  }

  async me(): Promise<User> {
    return this.http.me();
  }

  async logout(): Promise<void> {
    await this.http.logout();
    this.setTokenFn(null);
  }

  async wsTicket(): Promise<WSTicket> {
    return this.http.wsTicket();
  }

  async listChannels(): Promise<Channel[]> {
    return this.http.listChannels();
  }

  async createChannel(name: string): Promise<Channel> {
    return this.http.createChannel(name);
  }

  async renameChannel(id: string, name: string): Promise<Channel> {
    return this.http.renameChannel(id, name);
  }

  async listMessages(channelId: string, opts?: ListMessagesOptions): Promise<Message[]> {
    return this.http.listMessages(channelId, opts);
  }

  async postMessage(channelId: string, body: string): Promise<Message> {
    return this.http.postMessage(channelId, body);
  }

  watch(
    channelId: string,
    opts: { signal?: AbortSignal } = {},
  ): AsyncGenerator<WsEvent, void, void> {
    return watchGen(this.http, channelId, {
      WebSocket: this.wsCtor,
      signal: opts.signal,
    });
  }

  websocket(channelId?: string): WebSocketClient {
    return new WebSocketClient({
      http: this.http,
      channelId,
      WebSocket: this.wsCtor,
    });
  }
}

export function createClient(opts: ClientOptions): Client {
  return new Client(opts);
}
