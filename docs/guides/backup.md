---
description: Back up piecemeal's database while it runs, and restore it, keeping settings, users, API keys, torrents and RSS.
---

# Backup and restore

The database in `PIECEMEAL_DATA_DIR` holds settings, users, API keys, torrents, categories, tags and RSS. Back it up while piecemeal runs, either from **Settings > Backup** in the web UI (admins), with `GET /api/piecemeal/v1/admin/backup`, or from the command line:

```sh
docker exec piecemeal piecemeal backup /data/piecemeal-backup.db
```

A backup is a consistent copy of the SQLite database, taken without stopping piecemeal. Treat it as a secret: it holds the hashes of sessions and API keys, and cookies for trackers.

## Restore

Stop piecemeal and run `piecemeal restore FILE` with the same data directory. The database it replaces is kept next to it with a `.replaced-` suffix, so a restore can be undone.

```sh
docker stop piecemeal
docker run --rm -v piecemeal-data:/data -v "$PWD":/backup \
  ghcr.io/sebdanielsson/piecemeal restore /backup/piecemeal-backup.db
docker start piecemeal
```

`restore` checks that the file is a piecemeal database it can migrate, and refuses while piecemeal runs. Only one piecemeal can use a data directory at a time; it holds a lock on it.

## What isn't in the backup

Downloaded data isn't part of the backup: it stays where it is. Piece completion, which records what's been verified, lives beside the database but isn't backed up either. After a restore on a new machine, piecemeal checks the data again, running torrents at the next start and stopped ones when they start, rather than downloading it again.
