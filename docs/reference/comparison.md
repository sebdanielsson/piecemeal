---
description: How piecemeal compares with qBittorrent, Transmission and Deluge as a server's torrent client, feature by feature, including where it falls short.
---

# Comparison

piecemeal against the three clients people most often run on a server, compared as servers run them: qBittorrent 5.3.0 as `qbittorrent-nox`, Transmission 4.1.4 as `transmission-daemon`, and Deluge 2.2.0 as `deluged` with `deluge-web`. The other clients were compared from their tagged source and documentation in November 2026, not run side by side. Speed and memory aren't compared, because there's no fair benchmark of all four; see [Performance](performance.md) for piecemeal's own numbers.

If something here is wrong or out of date, [open an issue](https://github.com/sebdanielsson/piecemeal/issues).

## Which one should you pick?

- **piecemeal** if Radarr, Sonarr and Prowlarr drive your downloads on a server, several people use it, and you want SSO, scoped API keys, metrics and VPN port forwarding built in. Not if you need per-torrent speed limits, a search engine, or a private tracker that only allows clients on its list.
- **qBittorrent** if you want the most features: search, I2P, per-torrent limits, super seeding and SSL torrents, a desktop app, and a client private trackers widely allow. The \*arrs support it best, which is why piecemeal speaks its API.
- **Transmission** if you want something small and simple with a web UI that works well on a phone. It has no RSS, no search and no categories, and the \*arrs use its labels instead.
- **Deluge** if you want plugins or the desktop thin client against a daemon. It has had no release since April 2025.

## Running it

| | piecemeal | qBittorrent | Transmission | Deluge |
| --- | --- | --- | --- | --- |
| Runs as | One Go binary | `qbittorrent-nox` (C++, Qt) | `transmission-daemon` (C++) | `deluged` and `deluge-web` (Python) |
| Torrent engine | anacrolix/torrent | libtorrent-rasterbar | libtransmission | libtorrent-rasterbar |
| Official container image | Yes, signed, with SBOMs | Yes | No; linuxserver.io's | No; linuxserver.io's |
| Desktop app | No; the web UI only | Yes (Qt) | Yes (GTK, Qt, macOS) | Yes (GTK thin client, console) |
| Built-in HTTPS | No; use a reverse proxy | Yes | No | Yes, in `deluge-web` |
| Prometheus metrics | Built in | Third-party exporter | Third-party exporter | Third-party exporter |
| Health and readiness checks | Yes | No | No | No |
| Import torrents from other clients | From Transmission and qBittorrent, with their data | No | No | No |
| Plugins | No | Search plugins only | No; scripts on events | Yes, in Python, many bundled |
| License | MIT | GPL-2.0-or-later | GPL-2.0 or GPL-3.0 | GPL-3.0-or-later |
| First release | 2026 | 2006 | 2005 | 2006 |
| Releases in 2026 | Its first, from 0.1 to 1.x | 6 | 5 | None; the last was in April 2025 |

## Web UI and access

| | piecemeal | qBittorrent | Transmission | Deluge |
| --- | --- | --- | --- | --- |
| Web UI | Made for the browser, with a phone layout | Desktop layout; usable on a phone | Rewritten for phones in 4.0 | Desktop only |
| Alternative web UIs | VueTorrent, tested; other qBittorrent UIs untested | VueTorrent, iQbit, Flood | Flood, TrguiNG | Flood (experimental) |
| People | A local admin, and SSO users as admins or users | One account | One account | Daemon accounts for thin clients; one password for the web UI |
| Single sign-on (OpenID Connect) | Yes, with allowed and admin groups | No | No | No |
| API keys | Any number, scoped to everything, the qBittorrent API, or reading | One, with full access (5.2 and later) | No; username and password | No |
| API | Native REST API with OpenAPI, and qBittorrent's WebAPI v2 | WebAPI v2 | JSON-RPC | DelugeRPC, and JSON-RPC in `deluge-web` |

## With the \*arrs

| | piecemeal | qBittorrent | Transmission | Deluge |
| --- | --- | --- | --- | --- |
| Radarr, Sonarr and Prowlarr | As a qBittorrent client; tested in CI | Yes | Yes; labels stand in for categories | Yes, through `deluge-web`; categories need the Label plugin |
| Categories | Yes, with save paths | Yes, with save paths | No; labels | With the Label plugin, one per torrent |
| Tags | Yes | Yes | Labels, several per torrent | No |
| Save path follows the category (automatic torrent management) | Yes | Yes | No | No; a label can move completed downloads |
| Folder for incomplete downloads | Yes | Yes | Yes | Yes |
| Watch folder | Yes | Yes | Yes; not settable in the web UI | AutoAdd plugin |

## Downloading and seeding

| | piecemeal | qBittorrent | Transmission | Deluge |
| --- | --- | --- | --- | --- |
| Global speed limits | Yes, with alternative limits on a schedule | Yes, with alternative limits on a schedule | Yes, with alternative limits on a schedule | Yes; a schedule with the bundled Scheduler plugin |
| Per-torrent speed limits | No ([#43](https://github.com/sebdanielsson/piecemeal/issues/43)) | Yes | Yes; not in the web UI | Yes |
| Queueing | Yes | Yes | Yes; the web UI sets only the download queue | Yes |
| Seeding goals | Ratio, seeding time and idle time, any or all of them, per torrent, category or globally; stop, remove, or remove with data | Ratio, seeding time and idle time, any or all of them, per torrent, category or globally; stop, remove, remove with data, or super seed | Ratio and idle time; stop | Ratio; stop, or remove |
| Sequential download | Yes | Yes | Yes (4.1 and later); not in the web UI | Yes; in the web UI, when adding |
| First and last pieces first | Yes | Yes | Through sequential download | Yes |
| File priorities and skipping files | Yes | Yes | Yes | Yes |
| Rename files and folders | Yes | Yes | Yes (web UI 4.1 and later) | Not in the web UI |
| RSS with download rules | Yes, with qBittorrent's rules | Yes | No | Third-party plugin (YaRSS2), desktop only |
| Search engine | No | Yes, with search plugins | No | No |
| Create torrents | Through the qBittorrent API, not in the web UI | Web UI and API | Command line and desktop app | Through the daemon's API; not in the web UI |
| Tracker status | No; trackers show as not contacted ([#44](https://github.com/sebdanielsson/piecemeal/issues/44)) | Yes | Yes | Yes |

## Network

| | piecemeal | qBittorrent | Transmission | Deluge |
| --- | --- | --- | --- | --- |
| Encryption, uTP, DHT, peer exchange | Yes | Yes | Yes | Yes; uTP can't be turned off |
| Local service discovery | No ([#57](https://github.com/sebdanielsson/piecemeal/issues/57)) | Yes | Yes | Yes |
| UPnP and NAT-PMP | UPnP; NAT-PMP to a gateway you set | Yes | Yes | Yes |
| Port forwarding through a VPN's NAT-PMP gateway, such as ProtonVPN's | Built in; follows the port when it changes | With an external script or tool | With an external script or tool | With an external script or tool |
| Bind to a VPN's network interface, so traffic stops when it goes down | Yes, every BitTorrent connection, with the interface's state in the status bar and `/readyz` | Yes, to an interface or an address | To an address only | Yes, an outgoing interface |
| Peer proxy | HTTP, SOCKS5; the DHT and uTP are off while peers go through it ([Proxy](../guides/proxy.md)) | HTTP, SOCKS4, SOCKS5 | No; an HTTP proxy for trackers and web seeds only | HTTP, SOCKS4, SOCKS5, I2P |
| I2P | No ([#47](https://github.com/sebdanielsson/piecemeal/issues/47)) | Yes (experimental) | No | As a proxy |
| IP filter | A filter file and banned addresses | A filter file and banned addresses | A blocklist URL, updated automatically | Blocklist plugin, from a URL, updated automatically |
| Super seeding | No ([#46](https://github.com/sebdanielsson/piecemeal/issues/46)) | Yes | No | Yes |
| SSL torrents | No ([#48](https://github.com/sebdanielsson/piecemeal/issues/48)) | Yes | No | No |
| BitTorrent v2 and hybrid torrents | Yes, in the engine; less tested than v1 | Yes | Hybrids through their v1 side; pure v2 torrents are refused | Depends on the libtorrent it's built with |
| Web seeds | Yes, editable | Yes, editable (5.1 and later) | Yes | Used, but not shown |
| Private trackers that allow only listed clients | Not on their lists; see the [FAQ](../help/faq.md#does-it-support-private-trackers) | Widely allowed | Widely allowed | Widely allowed |
