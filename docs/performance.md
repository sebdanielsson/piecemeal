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

1,000 torrents of 1 MiB, 256 KiB pieces, on an Apple M5 with an SSD (APFS), DHT off, no peers:

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

Web UIs such as VueTorrent poll `sync/maindata` every one or two seconds, and the *arrs poll `torrents/info`. Both cost every torrent's state on each poll, so their cost grows with the number of torrents: at 1,000 torrents a web UI open in a browser costs about 1% of a core here.

The target deployment, three ageing 2 TB WD Green drives in RAID-Z1, will be slower to verify data and add torrents. Run the load test there with `DIR` on the pool before relying on these numbers.
