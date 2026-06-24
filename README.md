# HyPanel

**Self-hosted, Hysteria2-first VPN & proxy control panel — powered by [sing-box](https://github.com/SagerNet/sing-box).**

[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Vue](https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js&logoColor=white)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey)

> **Responsible use:** HyPanel is a server-administration tool for operators of their **own** VPN/proxy infrastructure. Use it only on systems you own or are authorized to manage, and in compliance with the laws and terms that apply to you.

**HyPanel** is an open-source web panel for running your own multi-protocol VPN/proxy server. It puts **Hysteria2** — a fast, censorship-resistant QUIC-based protocol — front and center, while supporting **VLESS, VMess, Trojan, Shadowsocks, TUIC, Naive, ShadowTLS** and more through an embedded **sing-box** core.

If you have used **3x-ui** or **s-ui**, HyPanel will feel familiar: per-client traffic accounting, quota and expiry limits, subscription links (link / JSON / Clash), live status monitoring, and a clean Vue 3 dashboard — with a focus on getting a secure, TLS-enabled node online with as little friction as possible.

## Why HyPanel

- **Hysteria2-first** — first-class support for Hysteria2 instead of burying it in a protocol dropdown.
- **Multi-protocol** — one panel, many protocols, via the embedded sing-box core.
- **Live user management** — add or remove clients on the fly without restarting the core.
- **Per-client accounting** — traffic caps, expiry dates, and live up/down statistics per client.
- **Subscription service** — shareable subscription links (link / JSON / Clash) with QR codes for mobile clients.
- **Multi-language** — English, Farsi, Vietnamese, Chinese (Simplified & Traditional), Russian.
- **Cross-platform** — Linux, Windows, and experimental macOS builds.

## Quick Overview

| Feature                                    |      Enabled?      |
| ------------------------------------------ | :----------------: |
| Hysteria2-first, multi-protocol            | :heavy_check_mark: |
| Multi-language                             | :heavy_check_mark: |
| Multi-client / multi-inbound               | :heavy_check_mark: |
| Advanced traffic routing interface         | :heavy_check_mark: |
| Client, traffic & system status            | :heavy_check_mark: |
| Subscription link (link / JSON / Clash)    | :heavy_check_mark: |
| Dark / light theme                         | :heavy_check_mark: |
| REST API                                   | :heavy_check_mark: |

## Supported Platforms

| Platform | Architecture | Status |
|----------|--------------|--------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Supported |
| Windows  | amd64, 386, arm64 | ✅ Supported |
| macOS    | amd64, arm64 | 🚧 Experimental |

## Supported Protocols

- **General:** Mixed, SOCKS, HTTP, HTTPS, Direct, Redirect, TProxy
- **V2Ray-based:** VLESS, VMess, Trojan, Shadowsocks
- **QUIC / modern:** Hysteria, **Hysteria2**, TUIC, Naive, ShadowTLS
- XTLS support, PROXY Protocol, external & transparent proxy, per-inbound TLS certificates

## Default Installation Information

- Panel port: `2095`
- Panel path: `/app/`
- Subscription port: `2096`
- Subscription path: `/sub/`
- Default user / password: `admin` / `admin` (change on first login)

## Quick Start (Docker, build from source)

> A one-line installer and prebuilt images ship with the first tagged release. Until then, build the image from source:

```sh
git clone https://github.com/OWNER/HyPanel
cd HyPanel
git submodule update --init --recursive
docker build -t hypanel .

docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 -p 80:80 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name hypanel --restart=unless-stopped \
    hypanel
```

Then open `http://<server-ip>:2095/app/` and sign in with `admin` / `admin`.

## Configuration (Environment Variables)

<details>
  <summary>Click for details</summary>

| Variable       |                      Type                      | Default  |
| -------------- | :--------------------------------------------: | :------- |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG      |                   `boolean`                    | `false`  |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`  |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`   |
| SINGBOX_API    |                    `string`                    | -        |

> Note: environment variables still use the `SUI_` prefix inherited from upstream; they will be renamed in a future release.

</details>

## SSL Certificate

<details>
  <summary>Click for details</summary>

Using Certbot (standalone):

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <your-domain>
```

</details>

## Building from Source (development)

<details>
  <summary>Click for details</summary>

The backend embeds sing-box and uses CGO; it is built on Linux. The frontend lives in the `frontend/` submodule (Vue 3 + Vuetify).

```sh
# Frontend
cd frontend && npm install && npm run build && cd ..
rm -fr web/html/* && cp -R frontend/dist/ web/html/

# Backend (Linux, CGO)
go build -o sui main.go
./sui
```

The supported and reproducible build path is the Docker build above.

</details>

## Credits & Attribution

HyPanel stands on the shoulders of excellent open-source work:

- **[s-ui](https://github.com/alireza0/s-ui)** by [alireza0](https://github.com/alireza0) — the upstream project HyPanel is derived from (GPL-3.0).
- **[sing-box](https://github.com/SagerNet/sing-box)** by [SagerNet](https://github.com/SagerNet) — the embedded universal proxy core.

Please consider supporting the upstream authors whose work makes this project possible.

## License

HyPanel is released under the **GPL-3.0** license — see [LICENSE](LICENSE). As a derivative of s-ui, it remains GPL-3.0 and preserves the original copyright and license notices.
