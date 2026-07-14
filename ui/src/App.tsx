import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import {
  api,
  AuthError,
  FileConfig,
  Session,
  Secret,
  clearAuth,
  setAuth,
  tgProxyLink,
} from "./api";

function newSecret(): Secret {
  return { id: crypto.randomUUID(), name: "", secret: "", enabled: true };
}

function isAuthError(e: unknown): boolean {
  return e instanceof AuthError || (e as Error)?.name === "AuthError";
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
  const [needLogin, setNeedLogin] = useState(false);
  const [loginUser, setLoginUser] = useState("admin");
  const [loginPass, setLoginPass] = useState("");
  const [loginError, setLoginError] = useState("");
  const [qrIdx, setQrIdx] = useState(0);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [version, setVersion] = useState("…");
  const [updateInfo, setUpdateInfo] = useState<{
    current: string;
    latest: string;
    update_available: boolean;
    arch: string;
    repo: string;
  } | null>(null);

  const load = useCallback(async () => {
    const res = await api.getConfig();
    setCfg(res.config);
    setNeedLogin(false);
    try {
      setSessions(await api.sessions());
    } catch {
      setSessions([]);
    }
  }, []);

  useEffect(() => {
    load().catch((e) => {
      if (isAuthError(e)) {
        setNeedLogin(true);
        setCfg(null);
        return;
      }
      setStatus({ kind: "err", text: String((e as Error).message || e) });
    });
    api
      .systemInfo()
      .then((info) => setVersion(info.version))
      .catch(() => undefined);
    const id = window.setInterval(() => {
      api
        .sessions()
        .then(setSessions)
        .catch((e) => {
          if (isAuthError(e)) setNeedLogin(true);
        });
    }, 5000);
    return () => clearInterval(id);
  }, [load]);

  const activeSecrets = useMemo(
    () => (cfg?.proxy.secrets || []).filter((s) => s.enabled && s.secret.trim()),
    [cfg]
  );

  const sharePort = cfg?.proxy.port ?? 8443;
  const qrSecret = activeSecrets[Math.min(qrIdx, Math.max(activeSecrets.length - 1, 0))];
  const shareLink = qrSecret
    ? tgProxyLink(serverHost || location.hostname, sharePort, qrSecret.secret)
    : "";

  useEffect(() => {
    let cancelled = false;
    if (!shareLink) {
      setQrDataUrl("");
      return;
    }
    QRCode.toDataURL(shareLink, {
      width: 360,
      margin: 1,
      color: { dark: "#071018", light: "#ffffff" },
    })
      .then((url) => {
        if (!cancelled) setQrDataUrl(url);
      })
      .catch(() => {
        if (!cancelled) setQrDataUrl("");
      });
    return () => {
      cancelled = true;
    };
  }, [shareLink]);

  const doLogin = async (e: FormEvent) => {
    e.preventDefault();
    setLoginError("");
    setAuth(loginUser, loginPass);
    try {
      await load();
    } catch (err: any) {
      clearAuth();
      setLoginError(err?.message || t("error"));
      setNeedLogin(true);
    }
  };

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
      if (isAuthError(e)) setNeedLogin(true);
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
    if (!secrets[idx].name) secrets[idx].name = `secret-${idx + 1}`;
    setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
    setStatus({ kind: "ok", text: "OK" });
  };

  const copyText = async (text: string, okMsg: string) => {
    await navigator.clipboard.writeText(text);
    setStatus({ kind: "ok", text: okMsg });
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

  if (needLogin) {
    return (
      <div className="app">
        <div className="brand" style={{ marginBottom: 24 }}>
          <h1>{t("title")}</h1>
          <p>{t("subtitle")}</p>
        </div>
        <section className="card" style={{ maxWidth: 420 }}>
          <h2>{t("login")}</h2>
          <form onSubmit={doLogin} className="grid" style={{ gridTemplateColumns: "1fr" }}>
            <label>
              {t("web.username")}
              <input
                autoFocus
                value={loginUser}
                onChange={(e) => setLoginUser(e.target.value)}
                autoComplete="username"
              />
            </label>
            <label>
              {t("web.password")}
              <input
                type="password"
                value={loginPass}
                onChange={(e) => setLoginPass(e.target.value)}
                autoComplete="current-password"
              />
            </label>
            {loginError ? <p className="toast err">{loginError}</p> : null}
            <button className="primary" type="submit">
              {t("login")}
            </button>
          </form>
        </section>
      </div>
    );
  }

  if (!cfg) {
    return (
      <div className="app">
        <p className="toast">{t("refresh")}…</p>
      </div>
    );
  }

  return (
    <div className="app">
      <div className="topbar">
        <div className="brand">
          <h1>{t("title")}</h1>
          <p>{t("subtitle")}</p>
          <div style={{ marginTop: 10 }}>
            <span className="badge on">{version}</span>
          </div>
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
          <button className="ghost" onClick={() => load()}>
            {t("refresh")}
          </button>
          <button
            className="ghost"
            onClick={() => {
              clearAuth();
              setNeedLogin(true);
              setCfg(null);
            }}
          >
            {t("logout")}
          </button>
          <button className="primary" disabled={busy} onClick={save}>
            {t("save")}
          </button>
        </div>
      </div>

      <div className="hero">
        <section className="card status-hero">
          <div className="status-row">
            <span className={`dot ${cfg.proxy.enabled ? "on" : ""}`} />
            <div className="status-title">{cfg.proxy.enabled ? t("online") : t("offline")}</div>
          </div>
          <div className="kpi">
            <div>
              <strong>{sharePort}</strong>
              {t("proxy.port")}
            </div>
            <div>
              <strong>{sessions.length}</strong>
              {t("clients")}
            </div>
            <div>
              <strong>{activeSecrets.length}</strong>
              secrets
            </div>
          </div>
          <p className="note" style={{ marginTop: 14 }}>
            {t("nfqws.body")}
          </p>
        </section>

        <section className="card">
          <h2>{t("share.title")}</h2>
          <label>
            {t("share.hint")}
            <input value={serverHost} onChange={(e) => setServerHost(e.target.value)} placeholder="vpn.example.com" />
          </label>
          {activeSecrets.length > 1 ? (
            <label style={{ marginTop: 10 }}>
              Secret
              <select value={String(Math.min(qrIdx, activeSecrets.length - 1))} onChange={(e) => setQrIdx(Number(e.target.value))}>
                {activeSecrets.map((s, i) => (
                  <option key={s.id || i} value={i}>
                    {s.name || `secret-${i + 1}`}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <div className="qr-wrap" style={{ marginTop: 12 }}>
            {qrDataUrl ? (
              <img src={qrDataUrl} alt="Telegram proxy QR" />
            ) : (
              <p className="empty">{t("share.noSecret")}</p>
            )}
          </div>
          <p className="qr-caption">{t("share.scan")}</p>
          {shareLink ? <div className="link-box">{shareLink}</div> : null}
          <div className="actions">
            <button
              className="primary"
              disabled={!shareLink}
              onClick={() => copyText(shareLink, t("secrets.copied"))}
            >
              {t("share.copyLink")}
            </button>
            <button
              className="ghost"
              disabled={!qrSecret}
              onClick={() => qrSecret && copyText(qrSecret.secret, t("secrets.copied"))}
            >
              {t("share.copySecret")}
            </button>
            <a
              className="ghost"
              style={{
                display: "inline-flex",
                alignItems: "center",
                textDecoration: "none",
                padding: "10px 12px",
                borderRadius: 10,
                border: "1px solid var(--line)",
                color: "var(--text)",
                pointerEvents: shareLink ? "auto" : "none",
                opacity: shareLink ? 1 : 0.5,
              }}
              href={shareLink || undefined}
            >
              {t("share.openTelegram")}
            </a>
          </div>
        </section>
      </div>

      <section className="card">
        <h2>{t("remote.title")}</h2>
        <p className="note">{t("remote.body")}</p>
        <ol className="steps" style={{ marginTop: 10 }}>
          <li>{t("remote.steps1")} <code>{sharePort}</code></li>
          <li>{t("remote.steps2")}</li>
          <li>{t("remote.steps3")}</li>
          <li>{t("remote.steps4")}</li>
        </ol>
        <div className="actions">
          <button
            className="ghost"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                const res = await api.publicIP();
                setServerHost(res.ip);
                setStatus({ kind: "ok", text: res.ip });
              } catch (e: any) {
                setStatus({ kind: "err", text: e.message || t("error") });
              } finally {
                setBusy(false);
              }
            }}
          >
            {t("remote.detect")}
          </button>
        </div>
      </section>

      <section className="card">
        <h2>{t("update.title")}</h2>
        <div className="row" style={{ marginBottom: 10 }}>
          <span className="badge">
            {t("update.current")}: {updateInfo?.current || version}
          </span>
          {updateInfo ? (
            <span className={`badge ${updateInfo.update_available ? "on" : ""}`}>
              {t("update.latest")}: {updateInfo.latest}
              {" · "}
              {updateInfo.update_available ? t("update.available") : t("update.uptodate")}
            </span>
          ) : null}
        </div>
        <div className="actions">
          <button
            className="ghost"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                const res = await api.updateCheck();
                setUpdateInfo(res.check);
                setVersion(res.check.current);
                setStatus({
                  kind: "ok",
                  text: res.check.update_available ? t("update.available") : t("update.uptodate"),
                });
              } catch (e: any) {
                setStatus({ kind: "err", text: e.message || t("error") });
              } finally {
                setBusy(false);
              }
            }}
          >
            {t("update.check")}
          </button>
          <button
            className="primary"
            disabled={busy || !updateInfo?.update_available}
            onClick={async () => {
              setBusy(true);
              setStatus({ kind: "", text: t("update.restarting") });
              try {
                await api.updateApply();
                window.setTimeout(() => location.reload(), 18000);
              } catch (e: any) {
                setStatus({ kind: "err", text: e.message || t("error") });
                setBusy(false);
              }
            }}
          >
            {t("update.apply")}
          </button>
        </div>
      </section>

      <section className="card">
        <h2>{t("proxy.title")}</h2>
        <label className="check">
          <input
            type="checkbox"
            checked={cfg.proxy.enabled}
            onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, enabled: e.target.checked } })}
          />
          {t("proxy.enabled")}
        </label>
        <div className="grid" style={{ marginTop: 12 }}>
          <label>
            {t("proxy.port")}
            <input
              type="number"
              value={cfg.proxy.port}
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, port: Number(e.target.value) } })}
            />
          </label>
          <label>
            {t("proxy.bind")}
            <input
              value={cfg.proxy.bind_address}
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, bind_address: e.target.value } })}
            />
          </label>
          <label>
            {t("proxy.fakeSni")}
            <input
              value={cfg.proxy.fake_sni}
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, fake_sni: e.target.value } })}
            />
          </label>
          <label>
            {t("proxy.upstream")}
            <select
              value={cfg.proxy.upstream_mode || "auto"}
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, upstream_mode: e.target.value } })}
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
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, cfworker_domain: e.target.value } })}
            />
          </label>
          <label>
            {t("proxy.dcRelay")}
            <input
              value={cfg.proxy.dc_relay}
              placeholder="host:443"
              onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, dc_relay: e.target.value } })}
            />
          </label>
        </div>
        <label className="check" style={{ marginTop: 12 }}>
          <input
            type="checkbox"
            checked={cfg.proxy.cfproxy_enabled}
            onChange={(e) => setCfg({ ...cfg, proxy: { ...cfg.proxy, cfproxy_enabled: e.target.checked } })}
          />
          {t("proxy.cfProxy")}
        </label>
        <div className="actions">
          <button className="ghost" disabled={busy} onClick={testConn}>
            {t("proxy.test")}
          </button>
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
        {cfg.proxy.secrets.map((s, idx) => (
          <div className={`secret ${qrSecret?.id === s.id ? "active" : ""}`} key={s.id || idx}>
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
              <button className="ghost" onClick={() => generate(idx)}>
                {t("secrets.generate")}
              </button>
              <button
                className="ghost"
                disabled={!s.secret}
                onClick={() =>
                  copyText(tgProxyLink(serverHost, sharePort, s.secret), t("secrets.copied"))
                }
              >
                {t("secrets.copyLink")}
              </button>
              <button
                className="primary"
                disabled={!s.secret || !s.enabled}
                onClick={() => {
                  const enabled = (cfg.proxy.secrets || []).filter((x) => x.enabled && x.secret.trim());
                  const pos = enabled.findIndex((x) => x.id === s.id);
                  setQrIdx(pos >= 0 ? pos : 0);
                }}
              >
                {t("secrets.useForQr")}
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
              onChange={(e) => setCfg({ ...cfg, web: { ...cfg.web, port: Number(e.target.value) } })}
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
              onChange={(e) => setCfg({ ...cfg, web: { ...cfg.web, username: e.target.value } })}
            />
          </label>
          <label>
            {t("web.password")}
            <input
              type="password"
              value={cfg.web.password}
              onChange={(e) => setCfg({ ...cfg, web: { ...cfg.web, password: e.target.value } })}
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
                  <td className="mono">
                    {s.client_ip}:{s.client_port}
                  </td>
                  <td>{s.name || s.id}</td>
                  <td className="mono">{s.destination}</td>
                  <td>{new Date(s.connected_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <p className={`toast ${status.kind}`}>{status.text}</p>
    </div>
  );
}
