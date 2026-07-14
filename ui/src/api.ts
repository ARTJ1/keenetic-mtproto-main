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

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
  });
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
};

export function tgProxyLink(server: string, port: number, secret: string): string {
  const host = server.trim();
  return `tg://proxy?server=${encodeURIComponent(host)}&port=${port}&secret=${encodeURIComponent(secret)}`;
}
