---
description: Send piecemeal's tracker announces, peer connections, torrent downloads and RSS feeds through a SOCKS5 or HTTP proxy, and what can't go through one.
---

# Proxy

piecemeal can send its traffic through a SOCKS5 or HTTP proxy, such as one a VPN provider offers, for when it can't run inside a VPN. Where you can, run it inside the VPN instead: everything goes through the tunnel, the DHT and µTP included, and peers can reach you through a forwarded port. See [VPN and port forwarding](vpn.md).

## Setting it up

In the web UI, open **Settings**, choose the proxy type under **Proxy**, and enter its host, its port, and a username and password if it asks for them. Or pin the settings with environment variables:

```yaml
environment:
  PIECEMEAL_PROXY_TYPE: socks5
  PIECEMEAL_PROXY_HOST: 10.64.0.1
  PIECEMEAL_PROXY_PORT: "1080"
  PIECEMEAL_PROXY_USERNAME: piecemeal
  PIECEMEAL_PROXY_PASSWORD: ${PROXY_PASSWORD}
```

Changes apply at once: the torrent client restarts with the new settings, and torrents carry on.

## What goes through it

Once a proxy is set, everything below goes through it. Each row can be turned off with its setting.

| Traffic | Setting | Through the proxy |
| --- | --- | --- |
| Tracker announces over HTTP, HTTPS and WebSocket, and web seeds | `proxyBitTorrent` | Yes. |
| Tracker announces over UDP | `proxyBitTorrent` | Through a SOCKS5 proxy's UDP relay. An HTTP proxy, or a SOCKS5 proxy that doesn't relay UDP, can't carry them, so they fail. |
| Connections to peers | `proxyPeerConnections` | Yes, over TCP. The DHT, µTP and WebRTC peers can't go through a proxy, so they're off meanwhile. |
| Torrent files from URLs: added by URL, by the \*arrs, or by RSS rules | `proxyTorrentFiles` | Yes, except to addresses on `PIECEMEAL_FETCH_ALLOWED_NETWORKS`. |
| RSS feeds | `proxyRSS` | Yes, except to addresses on `PIECEMEAL_FETCH_ALLOWED_NETWORKS`. |

Nothing set to use the proxy goes around it. When the proxy is down, refuses a connection or can't carry it, the connection fails; piecemeal never falls back to connecting directly.

Addresses on `PIECEMEAL_FETCH_ALLOWED_NETWORKS` are private, such as Prowlarr's on your network, so a proxy elsewhere couldn't reach them: piecemeal connects to them directly.

## Incoming connections

piecemeal still listens on its peer port. Trackers see the proxy's address, so the peers they hand out can't reach you through it; only peers that already know your address, such as ones on your network, connect to you. A proxy can't forward a port, so with peer connections through it, piecemeal connects to peers but they rarely connect to it. Downloads work, and seeding works to peers you connect to, but you reach fewer peers than you would with a forwarded port.

## Hostname lookups

With `proxyHostnameLookup` on, the default, piecemeal passes the hostnames of trackers and web seeds to the proxy, so your DNS server doesn't see them. Two kinds are looked up by piecemeal itself:

- **UDP trackers**, because the torrent library looks them up before it sends.
- **Torrent files and RSS feeds**, so piecemeal can check the address against private networks first, as it does without a proxy (see [Security](../reference/security.md#fetching-urls)). The proxy then gets the address piecemeal checked, not the hostname.

## HTTP proxies

piecemeal asks an HTTP proxy for a tunnel with CONNECT for everything, plain HTTP trackers and peers on any port included. Squid and Privoxy allow CONNECT to port 443 only by default; allow the other ports, or use SOCKS5. SOCKS4 isn't supported, and neither are proxies that piecemeal would have to reach over HTTPS.

## The password

The proxy password is stored in piecemeal's database, and so in its backups. The web UI and both APIs never return it: reads show it empty, and an empty value leaves it as it is. Clearing the username removes it. To keep it out of the database, set it with `PIECEMEAL_PROXY_PASSWORD`.
