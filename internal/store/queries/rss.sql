-- name: ListRSSFolders :many
SELECT path FROM rss_folders ORDER BY path;

-- name: DeleteRSSFolders :exec
DELETE FROM rss_folders;

-- name: InsertRSSFolder :exec
INSERT INTO rss_folders (path) VALUES (sqlc.arg(path));

-- name: ListRSSFeeds :many
SELECT * FROM rss_feeds ORDER BY path;

-- name: UpsertRSSFeed :exec
INSERT INTO rss_feeds (uid, path, url, refresh_interval, title, last_build_date, last_error, fetched_at)
VALUES (sqlc.arg(uid), sqlc.arg(path), sqlc.arg(url), sqlc.arg(refresh_interval), sqlc.arg(title),
        sqlc.arg(last_build_date), sqlc.arg(last_error), sqlc.arg(fetched_at))
ON CONFLICT (uid) DO UPDATE SET
    path = excluded.path, url = excluded.url, refresh_interval = excluded.refresh_interval,
    title = excluded.title, last_build_date = excluded.last_build_date,
    last_error = excluded.last_error, fetched_at = excluded.fetched_at;

-- name: DeleteRSSFeed :exec
DELETE FROM rss_feeds WHERE uid = sqlc.arg(uid);

-- name: ListRSSArticles :many
SELECT * FROM rss_articles ORDER BY feed_uid, seq;

-- name: InsertRSSArticle :exec
INSERT OR IGNORE INTO rss_articles
    (feed_uid, id, seq, title, date, torrent_url, link, description, author, category, is_read)
VALUES (sqlc.arg(feed_uid), sqlc.arg(id), sqlc.arg(seq), sqlc.arg(title), sqlc.arg(date), sqlc.arg(torrent_url),
        sqlc.arg(link), sqlc.arg(description), sqlc.arg(author), sqlc.arg(category), sqlc.arg(is_read));

-- name: SetRSSArticleRead :exec
UPDATE rss_articles SET is_read = 1 WHERE feed_uid = sqlc.arg(feed_uid) AND id = sqlc.arg(id);

-- name: DeleteRSSArticle :exec
DELETE FROM rss_articles WHERE feed_uid = sqlc.arg(feed_uid) AND id = sqlc.arg(id);

-- name: ListRSSRules :many
SELECT name, def FROM rss_rules ORDER BY name;

-- name: PutRSSRule :exec
INSERT INTO rss_rules (name, def) VALUES (sqlc.arg(name), sqlc.arg(def))
ON CONFLICT (name) DO UPDATE SET def = excluded.def;

-- name: DeleteRSSRule :exec
DELETE FROM rss_rules WHERE name = sqlc.arg(name);
