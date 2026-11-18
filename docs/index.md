---
description: A self-hosted BitTorrent client written in Go, with a qBittorrent-compatible API for the *arrs, a modern web UI and single sign-on.
---

# Introduction

piecemeal is a self-hosted BitTorrent client. It runs as one static Go binary or container, keeps its state in SQLite, and serves three things on one port:

- **A web UI** for desktop and phone, built with React, with live updates, keyboard navigation and dark mode.
- **A native REST API** at `/api/piecemeal/v1`, described by an [OpenAPI document](https://piecemeal-flax.vercel.app/reference), with API keys and server-sent events.
- **The qBittorrent WebAPI v2** at `/api/v2`, so Radarr, Sonarr, Prowlarr and qBittorrent web UIs such as VueTorrent work with it as they do with qBittorrent 5.3.

The torrent engine is [anacrolix/torrent](https://github.com/anacrolix/torrent), a mature BitTorrent library for Go. piecemeal adds what a long-running home server needs around it: persistent state, queueing and seeding goals, categories and tags, RSS feeds with download rules, users with roles, SSO, an audit log, backups, and port forwarding through a VPN.

## Features

- **Drop-in for the \*arrs.** Add piecemeal to Radarr, Sonarr or Prowlarr as a qBittorrent download client, with an API key or a username and password. CI runs the real apps against it on every change.
- **Web UI.** Filter by state, category, tag and tracker; select and act on many torrents; inspect files, peers, trackers and a live piece map; drag a `.torrent` anywhere to add it. It works on a phone too.
- **Users and SSO.** A local admin from configuration, plus users from any OpenID Connect provider, with admin and allowed groups. API keys are scoped: full, qBittorrent API only, or read-only.
- **RSS.** Feeds in folders, and download rules with qBittorrent's matching: wildcards or regular expressions, episode filters and the smart episode filter.
- **Categories and paths.** Categories give their torrents a save path and seeding goals. Keep incomplete downloads in a folder of their own, and let automatic torrent management move a torrent's data when its category changes, as in qBittorrent.
- **Queueing and seeding goals.** Limit how many torrents download and seed at once, and stop or remove torrents at a ratio, a seeding time or an idle time, or only once every goal is met. Set goals globally, per category or per torrent.
- **Speed limits and schedule.** Global and alternative limits, with a schedule that switches between them.
- **VPN-friendly.** Run in a WireGuard container's network namespace and forward the peer port with NAT-PMP; piecemeal follows the port when the VPN changes it.
- **Safe by default.** Save paths are confined to allowed directories, fetches by URL refuse private addresses, sign-ins are rate limited, and every request from a browser is checked for CSRF. See [Security](reference/security.md).
- **Operable.** Prometheus metrics, health and readiness endpoints, structured logs, an in-app log and audit log, online backups, and import from Transmission and qBittorrent.

## What it doesn't do

Some things piecemeal doesn't have, so you know before you move:

- **Per-torrent speed limits.** anacrolix/torrent only limits the whole client, so piecemeal has global and alternative limits but none per torrent ([#43](https://github.com/sebdanielsson/piecemeal/issues/43)).
- **Proxies.** Peer and tracker connections don't go through a SOCKS or HTTP proxy ([#45](https://github.com/sebdanielsson/piecemeal/issues/45)). Use a VPN instead; see [VPN and port forwarding](guides/vpn.md).
- **Super seeding, I2P and SSL torrents** aren't supported ([#46](https://github.com/sebdanielsson/piecemeal/issues/46), [#47](https://github.com/sebdanielsson/piecemeal/issues/47), [#48](https://github.com/sebdanielsson/piecemeal/issues/48)). Their qBittorrent API endpoints answer that they aren't.
- **Tracker status.** Trackers show as "not contacted", because anacrolix/torrent doesn't report announce results ([#44](https://github.com/sebdanielsson/piecemeal/issues/44)).

The [qBittorrent compatibility](reference/qbittorrent-compat.md) page lists every qBittorrent endpoint and how piecemeal handles it.

## Next steps

- [Install piecemeal](install.md) with Docker, Compose or a binary.
- [Connect Radarr, Sonarr and Prowlarr](guides/arr.md).
- [Configure it](configuration.md): every environment variable and runtime setting.
- [Move from Transmission or qBittorrent](guides/import.md), keeping your torrents and their data.
