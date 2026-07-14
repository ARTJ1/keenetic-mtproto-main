# keenetic-mtproto

Standalone **Telegram MTProto proxy** for Keenetic (Entware) with a built-in web UI.

Clients add the proxy manually in Telegram (`server` / `port` / `secret`).  
Designed to run **next to [nfqws2-keenetic](https://github.com/nfqws/nfqws2-keenetic)** without fighting it.

## Why it does not conflict with nfqws2

| | nfqws2 | keenetic-mtproto |
|---|---|---|
| NFQUEUE | yes (~200) | **no** |
| iptables / mangle | yes | **no** |
| `SO_MARK` | `0x20000000` / `0x40000000` | **never set** |
| Init | `S51nfqws2` | `S95keenetic-mtproto` |
| Role | DPI desync for web | MTProto listen proxy for Telegram apps |

This process only opens TCP listeners (proxy + UI). Outbound traffic looks like any normal app.

## Install on Keenetic

1. Entware / OPKG must be installed.
2. After you publish a GitHub release, run on the router:

```sh
# set your repo path if different
export REPO="YOUR_GITHUB_USER/keenetic-mtproto"
curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/install.sh" | sh
```

Installs:

- binary → `/opt/sbin/keenetic-mtproto`
- config → `/opt/etc/keenetic-mtproto/config.json`
- service → `/opt/etc/init.d/S95keenetic-mtproto`

Defaults:

- Web UI: `http://<router-lan-ip>:7788` — login `admin` / `admin` (**change immediately**)
- Proxy port: **8443** (avoid 80/443 used by Keenetic itself)

3. In Keenetic UI: forward **TCP 8443** from WAN to the router (if you want remote friends to connect).
4. Open the web UI → generate a secret → copy `tg://proxy` link → paste into Telegram.

Service control:

```sh
/opt/etc/init.d/S95keenetic-mtproto start|stop|restart|status
```

## Config

```json
{
  "web": {
    "port": 7788,
    "bind": "0.0.0.0",
    "username": "admin",
    "password": "admin"
  },
  "proxy": {
    "enabled": true,
    "port": 8443,
    "bind_address": "0.0.0.0",
    "fake_sni": "storage.googleapis.com",
    "upstream_mode": "auto",
    "secrets": [],
    "cfproxy_enabled": true,
    "cfworker_domain": "",
    "dc_relay": ""
  }
}
```

`upstream_mode`: `auto` (WS then TCP), `ws`, or `tcp`.

On censored networks prefer `auto` and optionally a Cloudflare Worker domain.  
On a foreign VPS, `tcp` is usually enough.

## Build from source

```sh
# UI + binary
make build

# Keenetic release binaries
make release-local
```

Requires Go 1.22+ and Node 20+.

## Architecture

- MTProto core extracted from [B4](https://github.com/daniellavrushin/b4) (`mtproto` package): fake-TLS, obfuscated2, WS / CF upstream.
- **Not included:** transparent `mtproto-ws` bridge, NFQUEUE, routing tables.
- Single static binary (`CGO_ENABLED=0`) with embedded React UI (`go:embed`).

## License

GPLv3 — derived from B4’s MTProto implementation. See [LICENSE](LICENSE).

Upstream inspiration for WS / CF paths: [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
