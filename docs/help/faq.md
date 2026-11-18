---
description: Answers to common questions about piecemeal, from why it exists to how it compares with qBittorrent and Transmission.
---

# FAQ

## Why another BitTorrent client?

For a home server that the \*arrs drive, the usual choice is qBittorrent, whose web UI and API grew out of a desktop app, or Transmission, which the \*arrs support less well. piecemeal is built for that one job: a headless daemon with the qBittorrent API the \*arrs expect, a web UI designed for a browser, real users and SSO, and the operational pieces (metrics, health checks, backups, audit log, VPN port forwarding) a server needs.

## Is it a fork of qBittorrent?

No. piecemeal shares no code with qBittorrent, which is GPL. Its qBittorrent API is written from the API documentation and from the behaviour of a real qBittorrent 5.3, which CI compares it against. piecemeal is MIT licensed.

## Which apps work with it?

Radarr, Sonarr, Prowlarr and VueTorrent are tested in CI. Other tools that use qBittorrent's WebAPI v2, such as Lidarr or qbit_manage, should work too, but aren't tested. The [compatibility page](../reference/qbittorrent-compat.md) lists every endpoint, so you can check what a tool needs.

## Can I use it as a desktop torrent client?

Yes, on macOS or Linux: run the binary and open `http://localhost:8080`. But it's a server first: there's no tray icon, and it doesn't open magnet links from the browser on its own.

## How many torrents can it handle?

The load test adds 1,000 torrents with their data and measures the rest; see [Performance](../reference/performance.md). Idle CPU stays around 2% of one core and the Go heap around 30 MiB at 1,000 torrents. On slow disks, verifying data is the limit.

## Does it support private trackers?

Private torrents work: they use neither DHT nor peer exchange, as the flag requires. But many private trackers only allow clients on their list, and piecemeal announces with anacrolix/torrent's peer ID prefix, `-GT0003-`, and tells peers it's piecemeal with its version. Check your tracker's rules before you use it there.

## Does it encrypt peer connections?

It prefers encrypted connections and accepts plain ones, by default. Set `encryption` to `require` to refuse plain ones, or `disable`.

## Where is my data?

Downloads are wherever their save paths point. piecemeal's own state, the database and piece completion, is in `PIECEMEAL_DATA_DIR`. Nothing leaves your server: piecemeal has no telemetry and makes no connections other than to trackers, peers, the DHT, URLs you add, RSS feeds, your SSO provider, and your router or VPN gateway for port forwarding.

## What's missing?

Per-torrent speed limits, proxies, super seeding, I2P, SSL torrents and tracker status, each tracked in an issue. See the [introduction](../index.md#what-it-doesnt-do).
