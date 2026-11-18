-- +goose Up

-- Per-torrent speed limits were stored but never enforced: anacrolix/torrent
-- only limits the whole client.
ALTER TABLE torrents DROP COLUMN download_limit;
ALTER TABLE torrents DROP COLUMN upload_limit;

-- +goose Down

ALTER TABLE torrents ADD COLUMN download_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN upload_limit INTEGER NOT NULL DEFAULT 0;
