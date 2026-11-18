---
sidebar:
  label: Import from other clients
description: Move your torrents from Transmission or qBittorrent to piecemeal, with their data, save paths, renames, categories, tags and totals.
---

# Move from Transmission or qBittorrent

`piecemeal import` reads another client's torrents and adds them, with their save paths, renamed and skipped files, labels or category and tags, stopped state, ratio limits, added and completed times, and transfer totals. Your data stays where it is.

Stop piecemeal first. The other client can keep running while you import, but stop it before piecemeal starts seeding the same files.

```sh
docker stop piecemeal
docker run --rm -v piecemeal-data:/data -v /path/to/downloads:/downloads \
  -v /path/to/transmission-config:/import:ro \
  ghcr.io/sebdanielsson/piecemeal import transmission /import --dry-run
```

Check the list it prints, then run it again without `--dry-run`, and start piecemeal.

- **Transmission**: give its configuration directory, the one with `torrents/` and `resume/`. Its labels become tags.
- **qBittorrent**: give its `BT_backup` directory, or the data directory that holds it. Its category and tags carry over.
- **`--map FROM=TO`** rewrites save paths when the other client saw the downloads under another path, such as `--map /data/torrents=/downloads`. Repeat it for several paths. Save paths must end up inside `PIECEMEAL_ALLOWED_ROOTS`.
- **Checking.** piecemeal checks each torrent's data when it next starts, and stopped ones when they start. `--trust-data` marks complete torrents as verified instead, which is much faster on large libraries but trusts the files on disk.
- **`--dry-run`** lists what would be imported and writes nothing.
- **Running it twice is safe**: torrents already in piecemeal are skipped.
- qBittorrent magnets that never got their metadata have no torrent file and are left out; add them again.

After the import, point the \*arrs at piecemeal instead of the old client; see [Radarr, Sonarr and Prowlarr](arr.md).
