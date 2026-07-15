# keenetic-mtproto

MTProto-прокси для Telegram на Keenetic (Entware) с веб-панелью.  
Клиенты подключаются вручную в Telegram: сервер / порт / secret.

Можно ставить рядом с [nfqws2-keenetic](https://github.com/nfqws/nfqws2-keenetic) — процессы не пересекаются (нет NFQUEUE / iptables / `SO_MARK`).

## Что нужно

1. Роутер Keenetic с **Entware / OPKG** (`/opt` должен существовать).
2. SSH-доступ к роутеру (или уже установленный curl/wget в Entware).

## Установка

На роутере:

```sh
curl -fsSL "https://raw.githubusercontent.com/ARTJ1/keenetic-mtproto-main/main/install.sh" | sh
```

Закрепить конкретный релиз:

```sh
curl -fsSL "https://raw.githubusercontent.com/ARTJ1/keenetic-mtproto-main/main/install.sh" | VERSION=v1.0.6 sh
```

Скрипт положит:

| Куда | Что |
|------|-----|
| `/opt/sbin/keenetic-mtproto` | бинарник |
| `/opt/etc/keenetic-mtproto/config.json` | конфиг |
| `/opt/etc/init.d/S95keenetic-mtproto` | сервис |

Архитектура определяется автоматически (`uname -m`): `mips`, `mipsel`, `armv7`, `aarch64`, `x86_64`.

## Первый запуск

1. Открой в браузере (с домашней сети):  
   `http://<IP-роутера>:7788`  
   Логин / пароль по умолчанию: **`admin` / `admin`** — сразу смени в настройках.

2. В панели сгенерируй **secret** и скопируй ссылку `tg://proxy…` (или QR).

3. В Telegram: **Настройки → Данные и память → Прокси → Добавить прокси → MTProto**  
   (или просто открой ссылку / отсканируй QR).

Порт прокси по умолчанию: **8443** (не 80/443 — их занимает сам Keenetic).

## Управление сервисом

```sh
/opt/etc/init.d/S95keenetic-mtproto start
/opt/etc/init.d/S95keenetic-mtproto stop
/opt/etc/init.d/S95keenetic-mtproto restart
/opt/etc/init.d/S95keenetic-mtproto status
```

## Доступ с мобильного интернета (не через Wi‑Fi роутера)

Панель (`:7788`) — только для LAN. Наружу её **не пробрасывай**.

Чтобы Telegram работал вне дома:

1. В веб-интерфейсе Keenetic: **проброс TCP 8443** с WAN на сам роутер.
2. В панели прокси укажи **публичный IP или DDNS** в поле host (или «Detect public IP»).
3. Заново скопируй / отсканируй `tg://proxy…` в Telegram.

## Обновление

В панели: **Updates → Check GitHub → Install & restart**  
или снова запусти `install.sh` по SSH (как при установке).

## Конфиг

Файл: `/opt/etc/keenetic-mtproto/config.json`

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

Полезные поля:

- `upstream_mode`: `auto` (сначала WS, потом TCP), `ws` или `tcp`  
  На цензуре обычно `auto`; на зарубежном VPS часто хватает `tcp`.
- `cfworker_domain` — домен Cloudflare Worker (опционально, если нужен WS/CF путь).
- Пустые `web.username` / `web.password` — панель без логина (только в LAN).

После правок конфига:

```sh
/opt/etc/init.d/S95keenetic-mtproto restart
```

## Сборка из исходников (необязательно)

Нужны Go 1.22+ и Node 20+.

```sh
make build          # UI + бинарник
make release-local  # архивы под Keenetic
```

Релизные архивы: `linux-mips_softfloat` (big-endian, `uname -m = mips`) и `linux-mipsle_softfloat` (little-endian, `mipsel`).

## Лицензия

GPLv3 — на базе MTProto-кода [B4](https://github.com/daniellavrushin/b4). См. [LICENSE](LICENSE).  
WS / CF пути вдохновлены [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).

