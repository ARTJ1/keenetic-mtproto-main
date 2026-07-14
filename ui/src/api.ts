export type Secret = {
  id: string;
  name: string;
  secret: string;
  enabled: boolean;
};

export type ProxyConfig = {
  enabled: boolean;
  port: number;
  bind_address: string;
  max_connections: number;
  tcp_user_timeout_sec: number;
  idle_timeout_sec: number;
  secrets: Secret[];
  fake_sni: string;
  dc_relay: string;
  upstream_mode: string;
  ws_custom_domain: string;
  ws_endpoint_host: string;
  cfproxy_enabled: boolean;
  cfproxy_url: string;
  cfworker_domain: string;
  dc_fallback_enabled: boolean;
  dc_fallback_url: string;
};

export type WebConfig = {
  port: number;
  bind: string;
  username: string;
  password: string;
};

export type FileConfig = {
  web: WebConfig;
  proxy: ProxyConfig;
  update?: { repo?: string };
};

export type Session = {
  id: string;
  name: string;
  client_ip: string;
  client_port: number;
  destination: string;
  connected_at: string;
  last_seen: string;
};

const AUTH_KEY = "kmt_auth";

export function getAuthHeader(): string | null {
  return sessionStorage.getItem(AUTH_KEY);
}

export function setAuth(username: string, password: string) {
  const token = btoa(`${username}:${password}`);
  sessionStorage.setItem(AUTH_KEY, `Basic ${token}`);
}

export function clearAuth() {
  sessionStorage.removeItem(AUTH_KEY);
}

export class AuthError extends Error {
  constructor(message = "unauthorized") {
    super(message);
    this.name = "AuthError";
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const auth = getAuthHeader();
  const res = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(auth ? { Authorization: auth } : {}),
      ...(init?.headers || {}),
    },
  });
  if (res.status === 401) {
    throw new AuthError("unauthorized");
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      msg = j.error || msg;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return res.json();
}

export const api = {
  getConfig: () => req<{ success: boolean; config: FileConfig }>("/api/config"),
  saveConfig: (config: FileConfig) =>
    req<{ success: boolean }>("/api/config", {
      method: "POST",
      body: JSON.stringify(config),
    }),
  generateSecret: (fake_sni: string) =>
    req<{ success: boolean; secret: string }>("/api/mtproto/generate-secret", {
      method: "POST",
      body: JSON.stringify({ fake_sni }),
    }),
  test: (body: Record<string, unknown> = {}) =>
    req<{ success: boolean; dc: number; results: unknown[] }>("/api/mtproto/test-ws", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  refreshDCs: () =>
    req<{ success: boolean }>("/api/mtproto/refresh-dcs", { method: "POST", body: "{}" }),
  sessions: () => req<Session[]>("/api/mtproto/sessions"),
  systemInfo: () =>
    req<{ success: boolean; version: string; arch: string; repo: string; proxy_port: number; web_port: number }>(
      "/api/system/info"
    ),
  publicIP: () => req<{ success: boolean; ip: string }>("/api/system/public-ip"),
  updateCheck: () =>
    req<{
      success: boolean;
      check: {
        current: string;
        latest: string;
        update_available: boolean;
        asset?: string;
        arch: string;
        repo: string;
      };
    }>("/api/update/check"),
  updateApply: () =>
    req<{ success: boolean; message: string }>("/api/update/apply", {
      method: "POST",
      body: "{}",
    }),
};

export function tgProxyLink(server: string, port: number, secret: string): string {
  const host = server.trim();
  return `tg://proxy?server=${encodeURIComponent(host)}&port=${port}&secret=${encodeURIComponent(secret)}`;
}
