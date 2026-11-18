---
description: The load test that measures piecemeal with 1,000 torrents, how to run it on your own disks, and its results.
---

# Performance

`test/load` measures piecemeal with many torrents. It writes the data for n single-file torrents, adds them with the data in place, waits until piecemeal has verified them all, and then measures memory, idle CPU and the endpoints that clients poll. Each timed request waits a second after the previous one, as clients poll, so a CPU that slows down between requests is part of the measurement.

## Running it

Against a piecemeal built from this checkout, with its data in a temporary directory:

```sh
test/load/run.sh                          # 1,000 torrents of 1 MiB
N=200 SIZE=4194304 test/load/run.sh       # fewer, larger torrents
DIR=/tank/load test/load/run.sh           # data on another disk, such as a ZFS pool
```

Against a running piecemeal, such as a deployment, with a full-scope API key and a directory inside its allowed roots:

```sh
PIECEMEAL_API_KEY=pm_... go run ./test/load -url https://piecemeal.example.com -dir /tank/downloads/load -n 1000
```

When piecemeal sees the directory under another path, as in a container, pass that path as `-server-dir`. `/metrics` must be on (`PIECEMEAL_METRICS=auth` or `public`). The tool removes the torrents and their data afterwards. With `-keep` they stay, so piecemeal can be restarted and measured again with `-measure`.

## Results

Two machines, with DHT off and no peers, 256 KiB pieces and 8 adds at a time.

### An old server with a ZFS pool

An Intel Core i7-2600 from 2011 with 15 GB of RAM, of which ZFS's cache (ARC) takes 8 GB, and the data on three ageing 2 TB WD Green drives in RAID-Z1, 80% full. 1,000 torrents of 1 MiB, then 1,000 torrents of 16 MiB: 16 GB, twice the ARC, so verifying reads the disks.

| Measure | 1,000 × 1 MiB | 1,000 × 16 MiB |
| --- | --- | --- |
| Adding | 2.6 s, 384 torrents/s | 3.5 s, 284 torrents/s |
| Add request | p50 8.4 ms, p95 42 ms | p50 20 ms, p95 60 ms |
| Until all are verified and seeding | 9 s after the last add | 4 min 10 s (65 MB/s) |
| Resident memory | 83 MiB | 118 MiB |
| Go heap in use | 44 MiB | 74 MiB |
| Idle CPU | 2.1% of one core | 2.4% of one core |
| `GET /api/piecemeal/v1/torrents` | p50 14 ms, 1.0 MiB | p50 11 ms, 1.0 MiB |
| `GET /api/v2/torrents/info` | p50 18 ms, 1.4 MiB | p50 15 ms, 1.4 MiB |
| `GET /api/v2/sync/maindata`, full | p50 53 ms, 1.5 MiB | p50 54 ms, 1.5 MiB |
| `GET /api/v2/sync/maindata`, incremental | p50 18 ms, 78 KiB | p50 15 ms, 80 KiB |
| Removing them with their data | 235 ms | 294 ms |

On the same server, 254 GiB in 20 torrents imported from Transmission verified at about 30 MB/s when piecemeal checked all 20 at once: the drives sought between them. API requests took about 45 ms meanwhile. piecemeal now checks one torrent at a time by default, as qBittorrent does (`maxActiveCheckingTorrents`); the others wait, queued for checking, and neither download nor seed until their data is checked.

### A laptop

An Apple M5 with an SSD (APFS), 1,000 torrents of 1 MiB:

| Measure | Result |
| --- | --- |
| Adding | 1.4 s, 735 torrents/s, 8 at a time |
| Add request | p50 4.1 ms, p95 36 ms |
| Until all are verified and seeding | 1.0 s after the last add |
| Idle CPU | 2.2% of one core |
| Go heap in use | 31 MiB |
| Goroutines | 23 |
| `GET /api/piecemeal/v1/torrents` | p50 8.4 ms, 1.1 MiB |
| `GET /api/v2/torrents/info` | p50 19 ms, 1.5 MiB |
| `GET /api/v2/sync/maindata`, full | p50 43 ms, 1.6 MiB |
| `GET /api/v2/sync/maindata`, incremental | p50 13 ms, 78 KiB |
| Removing them with their data | 175 ms |

The resident set size isn't reported on macOS; the Go runtime held 148 MiB from the OS, the same as with no torrents.

Web UIs such as VueTorrent poll `sync/maindata` every one or two seconds, and the \*arrs poll `torrents/info`. Both cost every torrent's state on each poll, so their cost grows with the number of torrents: at 1,000 torrents a web UI open in a browser costs about 1% of a core here.

Verifying data is bound by the disks: on drives like the ones above, plan on two to three hours per 250 GB when importing a library or after losing the piece completion state.
