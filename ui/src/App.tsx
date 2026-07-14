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

type Tab = "connect" | "proxy" | "access" | "system";
type ToastKind = "ok" | "err" | "info";
type Toast = { id: number; kind: ToastKind; text: string };

function newSecret(): Secret {
  return { id: crypto.randomUUID(), name: "", secret: "", enabled: true };
}

function isAuthError(e: unknown): boolean {
  return e instanceof AuthError || (e as Error)?.name === "AuthError";
}

function Tip({ text }: { text: string }) {
  return (
    <span className="tip" tabIndex={0} data-tip={text} aria-label={text}>
      ?
    </span>
  );
}

export default function App() {
  const { t, i18n } = useTranslation();
  const [cfg, setCfg] = useState<FileConfig | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [serverHost, setServerHost] = useState(location.hostname);
  const [busy, setBusy] = useState(false);
  const [needLogin, setNeedLogin] = useState(false);
  const [loginUser, setLoginUser] = useState("admin");
  const [loginPass, setLoginPass] = useState("");
  const [loginError, setLoginError] = useState("");
  const [qrIdx, setQrIdx] = useState(0);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [qrOpen, setQrOpen] = useState(false);
  const [tab, setTab] = useState<Tab>("connect");
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [version, setVersion] = useState("…");
  const [updateInfo, setUpdateInfo] = useState<{
    current: string;
    latest: string;
    update_available: boolean;
    arch: string;
    repo: string;
  } | null>(null);
  const [testOpen, setTestOpen] = useState(false);
  const [testPayload, setTestPayload] = useState<{ dc?: number; results: any[] } | null>(null);

  const toast = useCallback((kind: ToastKind, text: string) => {
    const id = Date.now() + Math.random();
    setToasts((prev) => [...prev.slice(-4), { id, kind, text }]);
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((x) => x.id !== id));
    }, 4200);
  }, []);

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
      toast("err", String((e as Error).message || e));
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
  }, [load, toast]);

  const activeSecrets = useMemo(
    () => (cfg?.proxy.secrets || []).filter((s) => s.enabled && s.secret.trim()),
    [cfg]
  );

  const sharePort = cfg?.proxy.port ?? 8443;
  const safeQrIdx = Math.min(qrIdx, Math.max(activeSecrets.length - 1, 0));
  const qrSecret = activeSecrets[safeQrIdx];
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
      width: 400,
      margin: 1,
      color: { dark: "#1a0a2e", light: "#ffffff" },
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

  useEffect(() => {
    if (!qrOpen && !testOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setQrOpen(false);
        setTestOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [qrOpen, testOpen]);

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

  const lang = i18n.language.startsWith("ru") ? "ru" : "en";

  const save = async () => {
    if (!cfg) return;
    setBusy(true);
    try {
      await api.saveConfig(cfg);
      toast("ok", t("saved"));
      await load();
    } catch (e: any) {
      if (isAuthError(e)) setNeedLogin(true);
      toast("err", e.message || t("error"));
    } finally {
      setBusy(false);
    }
  };

  const generate = async (idx: number) => {
    if (!cfg) return;
    const fake = cfg.proxy.fake_sni || "storage.googleapis.com";
    try {
      const res = await api.generateSecret(fake);
      const secrets = [...cfg.proxy.secrets];
      secrets[idx] = { ...secrets[idx], secret: res.secret };
      if (!secrets[idx].name) secrets[idx].name = `secret-${idx + 1}`;
      setCfg({ ...cfg, proxy: { ...cfg.proxy, secrets } });
      toast("ok", t("secrets.generate") + " · OK");
    } catch (e: any) {
      toast("err", e.message || t("error"));
    }
  };

  const copyText = async (text: string) => {
    await navigator.clipboard.writeText(text);
    toast("ok", t("copied"));
  };

  const openQrForSecret = (secret: Secret) => {
    const enabled = (cfg?.proxy.secrets || []).filter((x) => x.enabled && x.secret.trim());
    const pos = enabled.findIndex((x) => x.id === secret.id);
    if (pos < 0 || !secret.secret.trim()) {
      toast("err", t("toast.qrNeedSecret"));
      return;
    }
    setQrIdx(pos);
    setQrOpen(true);
  };

  const showQr = () => {
    if (!shareLink) {
      toast("err", t("toast.qrNeedSecret"));
      setTab("connect");
      return;
    }
    setQrOpen(true);
  };

  const testConn = async () => {
    if (!cfg) return;
    setBusy(true);
    toast("info", t("proxy.testing"));
    try {
      const res = await api.test({
        upstream_mode: cfg.proxy.upstream_mode,
        cfworker_domain: cfg.proxy.cfworker_domain,
        cfproxy_enabled: cfg.proxy.cfproxy_enabled,
        dc_relay: cfg.proxy.dc_relay,
      });
      const results = Array.isArray(res.results) ? res.results : [];
      const ok = results.some((r: any) => r.ok === true);
      setTestPayload({ dc: res.dc, results });
      setTestOpen(true);
      toast(ok ? "ok" : "err", ok ? t("toast.testOk") : t("toast.testFail"));
    } catch (e: any) {
      toast("err", e.message || t("error"));
    } finally {
      setBusy(false);
    }
  };

  const ToastHost = (
    <div className="toasts" aria-live="polite">
      {toasts.map((x) => (
        <div key={x.id} className={`toast-item ${x.kind}`}>
          {x.text}
        </div>
      ))}
    </div>
  );

  if (needLogin) {
    return (
      <div className="app login-wrap">
        {ToastHost}
        <section className="card login-card">
          <div className="brand" style={{ marginBottom: 18 }}>
            <h1>{t("title")}</h1>
            <p>{t("subtitle")}</p>
          </div>
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
            {loginError ? <p className="note" style={{ color: "var(--danger)" }}>{loginError}</p> : null}
            <button className="btn primary" type="submit">
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
        {ToastHost}
        <p className="note">{t("refresh")}…</p>
      </div>
    );
  }

  return (
    <div className="app">
      {ToastHost}

      <div className="topbar">
        <div className="brand">
          <h1>{t("title")}</h1>
          <p>{t("subtitle")}</p>
          <span className="ver-pill">{version}</span>
        </div>
        <div className="tools">
          <div className="lang-switch" role="group" aria-label="Language">
            <button type="button" className={lang === "ru" ? "active" : ""} onClick={() => setLang("ru")}>
              RU
            </button>
            <button type="button" className={lang === "en" ? "active" : ""} onClick={() => setLang("en")}>
              EN
            </button>
          </div>
          <button type="button" className="btn" onClick={() => load()}>
            {t("refresh")}
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => {
              clearAuth();
              setNeedLogin(true);
              setCfg(null);
            }}
          >
            {t("logout")}
          </button>
          <button type="button" className="btn primary" disabled={busy} onClick={save}>
            {t("save")}
          </button>
        </div>
      </div>

      <nav className="tabs">
        {(["connect", "proxy", "access", "system"] as Tab[]).map((id) => (
          <button
            key={id}
            type="button"
            className={tab === id ? "active" : ""}
            onClick={() => setTab(id)}
          >
            {t(`tabs.${id}`)}
          </button>
        ))}
      </nav>

      {tab === "connect" ? (
        <div className="panel">
          <section className="card status-card">
            <div className="status-row">
              <span className={`dot ${cfg.proxy.enabled ? "on" : ""}`} />
              <div className="status-title">{cfg.proxy.enabled ? t("online") : t("offline")}</div>
            </div>
            <div className="kpi">
              <div>
                <strong>{sharePort}</strong>
                {t("port")}
              </div>
              <div>
                <strong>{sessions.length}</strong>
                {t("clients")}
              </div>
              <div>
                <strong>{activeSecrets.length}</strong>
                {t("secretsCount")}
              </div>
            </div>
            <p className="note" style={{ marginTop: 14 }}>
              {t("nfqws.body")}
            </p>
          </section>

          <section className="card">
            <div className="card-head">
              <h2>{t("share.title")}</h2>
              <Tip text={t("hint.host")} />
            </div>
            <label>
              <span className="label-row">
                <span>{t("share.hint")}</span>
              </span>
              <input
                value={serverHost}
                onChange={(e) => setServerHost(e.target.value)}
                placeholder="vpn.example.com"
              />
            </label>
            {activeSecrets.length > 1 ? (
              <label style={{ marginTop: 10 }}>
                Secret
                <select
                  value={String(safeQrIdx)}
                  onChange={(e) => setQrIdx(Number(e.target.value))}
                >
                  {activeSecrets.map((s, i) => (
                    <option key={s.id || i} value={i}>
                      {s.name || `secret-${i + 1}`}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            {shareLink ? <div className="link-box">{shareLink}</div> : (
              <p className="empty">{t("share.noSecret")}</p>
            )}
            <div className="actions">
              <button type="button" className="btn primary" onClick={showQr} disabled={!shareLink}>
                {t("share.showQr")}
              </button>
              <button
                type="button"
                className="btn"
                disabled={!shareLink}
                onClick={() => copyText(shareLink)}
              >
                {t("share.copyLink")}
              </button>
              <button
                type="button"
                className="btn"
                disabled={!qrSecret}
                onClick={() => qrSecret && copyText(qrSecret.secret)}
              >
                {t("share.copySecret")}
              </button>
              <a
                className={`btn-link ${shareLink ? "" : "disabled"}`}
                href={shareLink || undefined}
              >
                {t("share.openTelegram")}
              </a>
            </div>
          </section>

          <section className="card">
            <div className="card-head">
              <h2>{t("secrets.title")}</h2>
              <Tip text={t("hint.secret")} />
            </div>
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
                  <button type="button" className="btn" onClick={() => generate(idx)}>
                    {t("secrets.generate")}
                  </button>
                  <button
                    type="button"
                    className="btn"
                    disabled={!s.secret}
                    onClick={() => copyText(tgProxyLink(serverHost, sharePort, s.secret))}
                  >
                    {t("secrets.copyLink")}
                  </button>
                  <button
                    type="button"
                    className="btn primary"
                    disabled={!s.secret || !s.enabled}
                    onClick={() => openQrForSecret(s)}
                  >
                    {t("secrets.useForQr")}
                  </button>
                  <button
                    type="button"
                    className="btn danger"
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
              type="button"
              className="btn"
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
        </div>
      ) : null}

      {tab === "proxy" ? (
        <div className="panel">
          <section className="card">
            <div className="card-head">
              <h2>{t("proxy.title")}</h2>
              <Tip text={t("hint.upstream")} />
            </div>
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
              <button type="button" className="btn primary" disabled={busy} onClick={testConn}>
                {t("proxy.test")}
              </button>
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={async () => {
                  try {
                    await api.refreshDCs();
                    toast("ok", t("toast.dcOk"));
                  } catch (e: any) {
                    toast("err", e.message || t("error"));
                  }
                }}
              >
                {t("proxy.refreshDcs")}
              </button>
            </div>
          </section>
        </div>
      ) : null}

      {tab === "access" ? (
        <div className="panel">
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
        </div>
      ) : null}

      {tab === "system" ? (
        <div className="panel">
          <section className="card">
            <h2>{t("remote.title")}</h2>
            <p className="note">{t("remote.body")}</p>
            <ol className="steps" style={{ marginTop: 10 }}>
              <li>
                {t("remote.steps1")} <code>{sharePort}</code>
              </li>
              <li>{t("remote.steps2")}</li>
              <li>{t("remote.steps3")}</li>
              <li>{t("remote.steps4")}</li>
            </ol>
            <div className="actions">
              <button
                type="button"
                className="btn primary"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    const res = await api.publicIP();
                    setServerHost(res.ip);
                    setTab("connect");
                    toast("ok", `${t("toast.ipOk")}: ${res.ip}`);
                  } catch (e: any) {
                    toast("err", e.message || t("error"));
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
            <div className="card-head">
              <h2>{t("update.title")}</h2>
              <Tip text={t("hint.update")} />
            </div>
            <div className="row" style={{ marginBottom: 10 }}>
              <span className="badge">
                {t("update.current")}: {updateInfo?.current || version}
              </span>
              {updateInfo ? (
                <span className={`badge ${updateInfo.update_available ? "on" : "ok"}`}>
                  {t("update.latest")}: {updateInfo.latest}
                  {" · "}
                  {updateInfo.update_available ? t("update.available") : t("update.uptodate")}
                </span>
              ) : null}
            </div>
            <div className="actions">
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    const res = await api.updateCheck();
                    setUpdateInfo(res.check);
                    setVersion(res.check.current);
                    toast(
                      "ok",
                      res.check.update_available ? t("update.available") : t("update.uptodate")
                    );
                  } catch (e: any) {
                    toast("err", e.message || t("error"));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                {t("update.check")}
              </button>
              <button
                type="button"
                className="btn primary"
                disabled={busy || !updateInfo?.update_available}
                onClick={async () => {
                  setBusy(true);
                  toast("info", t("update.restarting"));
                  try {
                    await api.updateApply();
                    window.setTimeout(() => location.reload(), 22000);
                  } catch (e: any) {
                    const msg = e?.message || t("error");
                    toast("err", msg);
                    setBusy(false);
                  }
                }}
              >
                {t("update.apply")}
              </button>
            </div>
          </section>
        </div>
      ) : null}

      {qrOpen ? (
        <div
          className="modal-backdrop"
          onClick={(e) => {
            if (e.target === e.currentTarget) setQrOpen(false);
          }}
        >
          <div className="modal" role="dialog" aria-modal="true" aria-label={t("share.qrTitle")}>
            <div className="modal-head">
              <h3>{t("share.qrTitle")}</h3>
              <button type="button" className="modal-close" onClick={() => setQrOpen(false)}>
                ×
              </button>
            </div>
            <div className="qr-big">
              {qrDataUrl ? (
                <img src={qrDataUrl} alt="Telegram proxy QR" />
              ) : (
                <p className="empty">{t("share.noSecret")}</p>
              )}
            </div>
            <p className="note" style={{ textAlign: "center", marginTop: 12 }}>
              {t("share.scan")}
              {qrSecret?.name ? ` · ${qrSecret.name}` : ""}
            </p>
            {shareLink ? <div className="link-box">{shareLink}</div> : null}
            <div className="actions">
              <button
                type="button"
                className="btn primary"
                disabled={!shareLink}
                onClick={() => copyText(shareLink)}
              >
                {t("share.copyLink")}
              </button>
              <a
                className={`btn-link ${shareLink ? "" : "disabled"}`}
                href={shareLink || undefined}
              >
                {t("share.openTelegram")}
              </a>
              <button type="button" className="btn" onClick={() => setQrOpen(false)}>
                {t("close")}
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {testOpen && testPayload ? (
        <div
          className="modal-backdrop"
          onClick={(e) => {
            if (e.target === e.currentTarget) setTestOpen(false);
          }}
        >
          <div className="modal wide" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>
                {t("proxy.testTitle")}
                {testPayload.dc != null ? ` · DC ${testPayload.dc}` : ""}
              </h3>
              <button type="button" className="modal-close" onClick={() => setTestOpen(false)}>
                ×
              </button>
            </div>
            <div className="test-list">
              {(testPayload.results || []).length === 0 ? (
                <p className="empty">{t("error")}</p>
              ) : (
                testPayload.results.map((r: any, i: number) => {
                  const ok = r?.ok === true;
                  return (
                    <div key={i} className={`test-item ${ok ? "ok" : "bad"}`}>
                      <div className="who">
                        {r?.mode || r?.name || r?.type || `result #${i + 1}`}
                        {r?.endpoint || r?.host ? ` · ${r.endpoint || r.host}` : ""}
                      </div>
                      <div>
                        {ok ? "OK" : r?.error || r?.message || JSON.stringify(r)}
                      </div>
                    </div>
                  );
                })
              )}
            </div>
            <div className="actions">
              <button type="button" className="btn primary" onClick={() => setTestOpen(false)}>
                {t("close")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
