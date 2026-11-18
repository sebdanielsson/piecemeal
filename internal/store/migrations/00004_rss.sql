-- +goose Up

-- RSS folders and feeds form a tree; paths join names with a backslash,
-- as in qBittorrent. The root folder has no row.
CREATE TABLE rss_folders (
    path TEXT PRIMARY KEY
) STRICT;

CREATE TABLE rss_feeds (
    uid              TEXT PRIMARY KEY,
    path             TEXT    NOT NULL UNIQUE,
    url              TEXT    NOT NULL UNIQUE,
    -- Seconds; 0 follows the global interval.
    refresh_interval INTEGER NOT NULL DEFAULT 0,
    title            TEXT    NOT NULL DEFAULT '',
    last_build_date  TEXT    NOT NULL DEFAULT '',
    last_error       TEXT    NOT NULL DEFAULT '',
    fetched_at       INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE rss_articles (
    feed_uid    TEXT    NOT NULL REFERENCES rss_feeds (uid) ON DELETE CASCADE,
    id          TEXT    NOT NULL,
    -- Arrival order within the feed.
    seq         INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    date        INTEGER NOT NULL,
    torrent_url TEXT    NOT NULL DEFAULT '',
    link        TEXT    NOT NULL DEFAULT '',
    description TEXT    NOT NULL DEFAULT '',
    author      TEXT    NOT NULL DEFAULT '',
    category    TEXT    NOT NULL DEFAULT '',
    is_read     INTEGER NOT NULL DEFAULT 0,
    -- Dropped past the per-feed limit, but still in the feed: kept so it
    -- doesn't come back as new on the next fetch.
    hidden      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_uid, id)
) STRICT;

-- Download rules, one JSON definition per name.
CREATE TABLE rss_rules (
    name TEXT PRIMARY KEY,
    def  TEXT NOT NULL
) STRICT;

-- +goose Down
DROP TABLE rss_rules;
DROP TABLE rss_articles;
DROP TABLE rss_feeds;
DROP TABLE rss_folders;
