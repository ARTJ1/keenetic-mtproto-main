import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  api,
  FileConfig,
  Session,
  Secret,
  tgProxyLink,
} from "./api";

function newSecret(): Secret {
  return {
    id: crypto.randomUUID(),
    name: "",
    secret: "",
    enabled: true,
  };
}

export default function App() {
  const { t, i18n } = useTranslation();
  const [cfg, setCfg] = useState<FileConfig | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [serverHost, setServerHost] = useState(location.hostname);
  const [status, setStatus] = useState<{ kind: "" | "ok" | "err"; text: string }>({
    kind: "",
    text: "",
  });
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const res = await api.getConfig();
    setCfg(res.config);
    try {
      setSessions(await api.sessions());
    } catch {
      setSessions([]);
    }
  }, []);

  useEffect(() => {
    load().catch((e) => setStatus({ kind: "err", text: String(e.message || e) }));
    const id = setInterval(() => {
      api.sessions().then(setSessions).catch(() => undefined);
    }, 5000);
    return () => clearInterval(id);
  }, [load]);

  const sharePort = cfg?.proxy.port ?? 8443;

  const setLang = (lng: string) => {
    i18n.changeLanguage(lng);
    localStorage.setItem("lang", lng);
  };

  const save = async () => {
    if (!cfg) return;
    setBusy(true);
    setStatus({ kind: "", text: t("saving") });
    try {
      await api.saveConfig(cfg);
      setStatus({ kind: "ok", text: t("saved") });
      await load();
    } catch (e: any) {
      setStatus({ kind: "err", text: e.message || t("error") });
    } finally {
      setBusy(false);
    }
  };

  const generate = async (idx: number) => {
    if (!cfg) return;
    const fake = cfg.proxy.fake_sni || "storage.googleapis.com";
    const res = await api.generateSecret(fake);
    const secrets = [...cfg.proxy.secrets];
    secrets[idx] = { ...secrets[idx], secret: res.secret };
    setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
  };

  const copyLink = async (secret: string) => {
    const link = tgProxyLink(serverHost, sharePort, secret);
    await navigator.clipboard.writeText(link);
    setStatus({ kind: "ok", text: t("secrets.copied") });
  };

  const testConn = async () => {
    if (!cfg) return;
    setBusy(true);
    setStatus({ kind: "", text: t("proxy.testing") });
    try {
      const res = await api.test({
        upstream_mode: cfg.proxy.upstream_mode,
        cfworker_domain: cfg.proxy.cfworker_domain,
        cfproxy_enabled: cfg.proxy.cfproxy_enabled,
        dc_relay: cfg.proxy.dc_relay,
      });
      const ok = (res.results || []).some((r: any) => r.ok === true);
      setStatus({
        kind: ok ? "ok" : "err",
        text: ok ? `DC ${res.dc}: OK` : JSON.stringify(res.results),
      });
    } catch (e: any) {
      setStatus({ kind: "err", text: e.message || t("error") });
    } finally {
      setBusy(false);
    }
  };

  const probeSummary = useMemo(() => status, [status]);

  if (!cfg) {
    return <div className="app"><p className="status">{t("refresh")}…</p></div>;
  }

  return (
    <div className="app">
      <header>
        <div className="brand">
          <h1>{t("title")}</h1>
          <p>{t("subtitle")}</p>
        </div>
        <div className="tools">
          <select
            aria-label={t("lang")}
            value={i18n.language.startsWith("ru") ? "ru" : "en"}
            onChange={(e) => setLang(e.target.value)}
          >
            <option value="ru">RU</option>
            <option value="en">EN</option>
          </select>
          <button className="ghost" onClick={() => load()}>{t("refresh")}</button>
          <button className="primary" disabled={busy} onClick={save}>{t("save")}</button>
        </div>
      </header>

      <section className="card">
        <h2>{t("nfqws.title")}</h2>
        <p className="note">{t("nfqws.body")}</p>
      </section>

      <section className="card">
        <h2>{t("proxy.title")}</h2>
        <label className="check">
          <input
            type="checkbox"
            checked={cfg.proxy.enabled}
            onChange={(e) =>
              setCfg({ ...cfg, proxy: { ...cfg.proxy, enabled: e.target.checked } })
            }
          />
          {t("proxy.enabled")}
        </label>
        <div className="grid" style={{ marginTop: 12 }}>
          <label>
            {t("proxy.port")}
            <input
              type="number"
              value={cfg.proxy.port}
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, port: Number(e.target.value) } })
              }
            />
          </label>
          <label>
            {t("proxy.bind")}
            <input
              value={cfg.proxy.bind_address}
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, bind_address: e.target.value } })
              }
            />
          </label>
          <label>
            {t("proxy.fakeSni")}
            <input
              value={cfg.proxy.fake_sni}
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, fake_sni: e.target.value } })
              }
            />
          </label>
          <label>
            {t("proxy.upstream")}
            <select
              value={cfg.proxy.upstream_mode || "auto"}
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, upstream_mode: e.target.value } })
              }
            >
              <option value="auto">{t("proxy.upstreamAuto")}</option>
              <option value="ws">{t("proxy.upstreamWs")}</option>
              <option value="tcp">{t("proxy.upstreamTcp")}</option>
            </select>
          </label>
          <label>
            {t("proxy.cfWorker")}
            <input
              value={cfg.proxy.cfworker_domain}
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, cfworker_domain: e.target.value } })
              }
            />
          </label>
          <label>
            {t("proxy.dcRelay")}
            <input
              value={cfg.proxy.dc_relay}
              placeholder="host:443"
              onChange={(e) =>
                setCfg({ ...cfg, proxy: { ...cfg.proxy, dc_relay: e.target.value } })
              }
            />
          </label>
        </div>
        <label className="check" style={{ marginTop: 12 }}>
          <input
            type="checkbox"
            checked={cfg.proxy.cfproxy_enabled}
            onChange={(e) =>
              setCfg({ ...cfg, proxy: { ...cfg.proxy, cfproxy_enabled: e.target.checked } })
            }
          />
          {t("proxy.cfProxy")}
        </label>
        <div className="actions">
          <button className="ghost" disabled={busy} onClick={testConn}>{t("proxy.test")}</button>
          <button
            className="ghost"
            disabled={busy}
            onClick={async () => {
              try {
                await api.refreshDCs();
                setStatus({ kind: "ok", text: "DC OK" });
              } catch (e: any) {
                setStatus({ kind: "err", text: e.message });
              }
            }}
          >
            {t("proxy.refreshDcs")}
          </button>
        </div>
      </section>

      <section className="card">
        <h2>{t("secrets.title")}</h2>
        <label>
          {t("secrets.serverHint")}
          <input value={serverHost} onChange={(e) => setServerHost(e.target.value)} />
        </label>
        <div style={{ marginTop: 12 }}>
          {cfg.proxy.secrets.map((s, idx) => (
            <div className="secret" key={s.id || idx}>
              <div className="grid">
                <label>
                  {t("secrets.name")}
                  <input
                    value={s.name}
                    onChange={(e) => {
                      const secrets = [...cfg.proxy.secrets];
                      secrets[idx] = { ...s, name: e.target.value };
                      setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
                    }}
                  />
                </label>
                <label className="check" style={{ marginTop: 22 }}>
                  <input
                    type="checkbox"
                    checked={s.enabled}
                    onChange={(e) => {
                      const secrets = [...cfg.proxy.secrets];
                      secrets[idx] = { ...s, enabled: e.target.checked };
                      setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
                    }}
                  />
                  {t("secrets.enabled")}
                </label>
              </div>
              <label>
                {t("secrets.secret")}
                <input
                  className="mono"
                  value={s.secret}
                  onChange={(e) => {
                    const secrets = [...cfg.proxy.secrets];
                    secrets[idx] = { ...s, secret: e.target.value };
                    setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
                  }}
                />
              </label>
              <div className="row">
                <button className="ghost" onClick={() => generate(idx)}>{t("secrets.generate")}</button>
                <button className="ghost" disabled={!s.secret} onClick={() => copyLink(s.secret)}>
                  {t("secrets.copyLink")}
                </button>
                <button
                  className="danger"
                  onClick={() => {
                    const secrets = cfg.proxy.secrets.filter((_, i) => i !== idx);
                    setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
                  }}
                >
                  {t("secrets.remove")}
                </button>
              </div>
            </div>
          ))}
        </div>
        <button
          className="ghost"
          onClick={() =>
            setCfg({
              ...cfg,
              proxy: { ...cfg.proxy, secrets: [...cfg.proxy.secrets, newSecret()] },
            })
          }
        >
          {t("secrets.add")}
        </button>
      </section>

      <section className="card">
        <h2>{t("web.title")}</h2>
        <p className="note">{t("web.note")}</p>
        <div className="grid" style={{ marginTop: 12 }}>
          <label>
            {t("web.port")}
            <input
              type="number"
              value={cfg.web.port}
              onChange={(e) =>
                setCfg({ ...cfg, web: { ...cfg.web, port: Number(e.target.value) } })
              }
            />
          </label>
          <label>
            {t("web.bind")}
            <input
              value={cfg.web.bind}
              onChange={(e) => setCfg({ ...cfg, web: { ...cfg.web, bind: e.target.value } })}
            />
          </label>
          <label>
            {t("web.username")}
            <input
              value={cfg.web.username}
              onChange={(e) =>
                setCfg({ ...cfg, web: { ...cfg.web, username: e.target.value } })
              }
            />
          </label>
          <label>
            {t("web.password")}
            <input
              type="password"
              value={cfg.web.password}
              onChange={(e) =>
                setCfg({ ...cfg, web: { ...cfg.web, password: e.target.value } })
              }
            />
          </label>
        </div>
      </section>

      <section className="card">
        <h2>{t("sessions.title")}</h2>
        {sessions.length === 0 ? (
          <p className="empty">{t("sessions.empty")}</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>{t("sessions.client")}</th>
                <th>{t("sessions.secret")}</th>
                <th>{t("sessions.dest")}</th>
                <th>{t("sessions.connected")}</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s, i) => (
                <tr key={`${s.id}-${s.client_ip}-${i}`}>
                  <td className="mono">{s.client_ip}:{s.client_port}</td>
                  <td>{s.name || s.id}</td>
                  <td className="mono">{s.destination}</td>
                  <td>{new Date(s.connected_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <p className={`status ${probeSummary.kind}`}>{probeSummary.text}</p>
    </div>
  );
}
