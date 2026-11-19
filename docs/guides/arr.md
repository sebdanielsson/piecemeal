---
description: Add piecemeal to Radarr, Sonarr and Prowlarr as a qBittorrent download client, with an API key, and let it fetch their download links.
---

# Radarr, Sonarr and Prowlarr

piecemeal speaks the qBittorrent WebAPI, so the \*arrs add it as a qBittorrent download client. CI runs Radarr 7, Sonarr 4 and Prowlarr 3 against piecemeal on every change: each runs its download client test, and Radarr grabs and imports a release through it.

## Add the download client

1. In piecemeal, open **API keys** and create a key with the **qBittorrent API** scope, named after the app.
2. In Radarr or Sonarr, go to **Settings > Download Clients**, add **qBittorrent**, and set:
   - **Host**: piecemeal's host name as the app reaches it, such as `piecemeal` on a shared Docker network.
   - **Port**: `8080`, or the port in `PIECEMEAL_ADDR`.
   - **API key**: the key. Leave the username and password empty. With an older app that has no API key field, use the admin's username and password.
   - **Category**: `radarr` or `sonarr`. piecemeal creates it on the first grab.
3. Press **Test**, then **Save**.

Prowlarr is added the same way under **Settings > Download Clients**, for grabs from its search page.

A key with the **qBittorrent API** scope works only on the qBittorrent API, not on piecemeal's own API or web UI, so a leaked \*arr config can't do more than the \*arr could. Make one key per app, so you can revoke one without the others.

## Paths

The \*arrs look for finished downloads at the path piecemeal reports. Mount the downloads at the same path in both containers, such as `/downloads`, and that path works for them as it is. If they must differ, add a **Remote Path Mapping** in the \*arr from piecemeal's path to its own.

A category can have its own save path: set one under **Categories & tags**, such as `/downloads/movies` for `radarr`. The \*arrs add torrents with their category and no save path, so they're saved there. Save paths must be inside `PIECEMEAL_ALLOWED_ROOTS`.

To keep unfinished downloads apart, turn on **Keep incomplete torrents apart** in **Settings**, or give a category its own incomplete folder. Torrents download there and move to their save path once complete; piecemeal reports them as moving until the data is in place, and only then as complete. The \*arrs don't need to see the incomplete folder.

With **Automatic management** on, in **Settings** for new torrents or per torrent, a torrent's paths follow its category: a category without a save path puts its torrents in a folder named after it in the default save path, such as `/downloads/radarr`. If the \*arr sets a **Post-Import Category**, the torrent's data then moves to that category's folder, unless **Move when the category changes** is off, which switches the torrent to manual management and leaves its data where the \*arr imported it from. Leave automatic management off if you don't want the \*arrs' category changes to move data. See [Save paths and the incomplete folder](../configuration.md#save-paths-and-the-incomplete-folder).

## Download links on private networks

Indexers often give the \*arrs a link to fetch rather than a magnet, and Prowlarr proxies those links through itself, as `http://prowlarr:9696/...`. piecemeal refuses to fetch from private addresses: links come from indexers, and fetching private addresses would let them reach into your network. Allow the networks the \*arrs and Prowlarr live on, such as the Docker network's subnet:

```yaml
    environment:
      PIECEMEAL_FETCH_ALLOWED_NETWORKS: 172.18.0.0/16
```

Without that, grabs fail with "address not allowed" in piecemeal's log, under **Log**. The error names the link without its query, which holds the indexer's API key.

## Seeding goals

The \*arrs' per-indexer seed ratio and seed time arrive as the torrent's own limits and override the goals of its category and the global ones in **Settings**. When a torrent reaches its goals, piecemeal stops it, or removes it, as its category or else `seedGoalAction` says. Leave that at stop for the \*arrs' categories: with **Remove Completed** on, the \*arr removes torrents itself once they're stopped after seeding, and it can't import a download piecemeal has already removed.

By default any one goal ends seeding. With **Reached when** set to every goal, in **Settings** or on the category, a torrent seeds until it has met both the ratio and the seed time it was given.

## Several instances

Radarr and Sonarr can share piecemeal: each tracks the torrents in its own category and leaves the rest alone. A second Radarr for 4K just needs another category, such as `radarr-4k`.
