---
description: Follow RSS and Atom feeds and download what matches, with qBittorrent's rule syntax, episode filters and the smart episode filter.
---

# RSS

piecemeal follows RSS and Atom feeds and adds the torrents that your download rules match. Feeds and rules work like qBittorrent's, so rules exported from qBittorrent import as they are, and qBittorrent tools that manage RSS work with piecemeal too.

Turn it on in **Settings**: `rssEnabled` fetches feeds on their schedule, and `rssAutoDownload` lets rules add torrents. Everyone can read feeds and refresh them; only admins change feeds and rules, since rules add torrents.

## Feeds

Add a feed under **RSS** with its URL, and optionally a folder and a name; without a name, it's named after its title once fetched. Feeds are fetched every `rssRefreshInterval` minutes, or at the feed's own interval, waiting `rssFetchDelay` seconds between requests to the same host. Each feed keeps its newest `rssMaxArticles` articles.

Feed URLs are fetched like torrent URLs: with the cookies set for their domain, and never from a private address unless `PIECEMEAL_FETCH_ALLOWED_NETWORKS` allows it. Trackers often put a passkey in a feed's URL, so people other than admins see feed URLs with the query hidden.

## Rules

A rule applies to the feeds you pick and decides, for each new article, whether to add its torrent.

- **Must contain** and **Must not contain** are wildcard terms by default: every space-separated term must match, `*` and `?` are wildcards, `[…]` is a set, and `|` separates alternatives. For example, `show 1080p` takes titles with both words, and `x264|x265` takes either.
- **Use regular expressions** makes them one regular expression each, PCRE-like: lookahead and inline flags such as `(?i)` work, and each match has a time limit so a runaway expression can't stall piecemeal.
- **Episode filter** takes a season and episodes: `1x2;8-15;20-;` takes season 1's episode 2, episodes 8 to 15 and 20 onwards. An open range continues into later seasons.
- **Smart episode filter** takes each episode once, recognizing it with the `rssSmartEpisodeFilters` setting (`S01E02`, `1x02` and dates by default). With `rssDownloadRepacks`, a REPACK or PROPER of an episode already taken is taken again.
- **Ignore days** skips matches for that many days after the rule's last match, so a release posted in several qualities is taken once.
- **Priority** orders the rules, lowest number first, as in qBittorrent. The first rule that matches an article takes it.

Each rule says how to add what it takes: a category, tags, a save path, whether to start, the content layout, and seeding goals with what happens at them. The rule editor previews which articles in its feeds it matches now.

Only unread articles are downloaded, newest first, and an article is marked read once its torrent is added. Unlike qBittorrent, piecemeal takes new articles even when a feed's `lastBuildDate` didn't change.

## Moving rules from qBittorrent

Export the rules from qBittorrent's RSS downloader to a JSON file, and upload it to the qBittorrent API:

```sh
curl -H "Authorization: Bearer $PIECEMEAL_API_KEY" \
  -F "rules=@qbittorrent-rules.json" \
  https://piecemeal.example.com/api/v2/rss/importRules
```

The key needs the full scope, since changing rules needs an admin. Rule options piecemeal doesn't use, such as `download_path`, are kept and returned as they were.
