import { TokenManager } from "@/auth/TokenManager";
import type { WSEvent } from "@/api/schemas";

type EventHandler = (event: WSEvent) => void;

export class WSClient {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers = new Map<string, Set<EventHandler>>();
  private subscriptions = new Set<string>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempts = 0;
  private maxReconnectDelay = 30_000;
  private baseDelay = 1_000;

  constructor(url: string) {
    this.url = url;
  }

  async connect(): Promise<void> {
    const token = await TokenManager.getInstance().getAccessToken();
    if (!token) return;

    const wsUrl = new URL(this.url, window.location.origin);
    wsUrl.searchParams.set("token", token);
    wsUrl.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";

    this.ws = new WebSocket(wsUrl.toString(), "iagent.web.v1");

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.resubscribe();
    };

    this.ws.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as WSEvent;
        this.handlers.get(event.type)?.forEach((h) => h(event));
        this.handlers.get("*")?.forEach((h) => h(event));
      } catch {
        // ignore parse errors
      }
    };

    this.ws.onclose = () => {
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      // onclose will fire after onerror
    };
  }

  subscribe(topic: string, handler: EventHandler) {
    this.subscriptions.add(topic);
    if (!this.handlers.has(topic)) {
      this.handlers.set(topic, new Set());
    }
    this.handlers.get(topic)!.add(handler);

    if (this.ws?.readyState === WebSocket.OPEN) {
      this.send({ type: "subscribe", topics: [topic] });
    }
  }

  unsubscribe(topic: string, handler: EventHandler) {
    this.handlers.get(topic)?.delete(handler);
    if (this.handlers.get(topic)?.size === 0) {
      this.handlers.delete(topic);
      this.subscriptions.delete(topic);
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.send({ type: "unsubscribe", topics: [topic] });
      }
    }
  }

  onAny(handler: EventHandler) {
    if (!this.handlers.has("*")) {
      this.handlers.set("*", new Set());
    }
    this.handlers.get("*")!.add(handler);
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }

  private resubscribe() {
    if (this.subscriptions.size > 0) {
      this.send({ type: "subscribe", topics: Array.from(this.subscriptions) });
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return;
    const delay = Math.min(
      this.baseDelay * Math.pow(2, this.reconnectAttempts),
      this.maxReconnectDelay
    );
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private send(msg: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }
}

const WS_PATH = import.meta.env.VITE_API_PREFIX ? `${import.meta.env.VITE_API_PREFIX}/ws` : '/ws';

let client: WSClient | null = null;

export function getWSClient(): WSClient {
  if (!client) {
    client = new WSClient(WS_PATH);
  }
  return client;
}

export function disconnectWS() {
  client?.disconnect();
  client = null;
}
