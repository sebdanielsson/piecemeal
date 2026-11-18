-- +goose Up

-- Changes to a torrent beyond what its .torrent file says. Empty means as
-- in the torrent file.
-- trackers: JSON array of tiers of announce URLs.
ALTER TABLE torrents ADD COLUMN trackers TEXT NOT NULL DEFAULT '';
-- web_seeds: JSON array of URLs.
ALTER TABLE torrents ADD COLUMN web_seeds TEXT NOT NULL DEFAULT '';
-- file_paths: JSON object from a file's path inside the torrent to the path
-- it was renamed to, both relative to the save path with / separators.
ALTER TABLE torrents ADD COLUMN file_paths TEXT NOT NULL DEFAULT '';
-- content_layout: Original, Subfolder or NoSubfolder, as in qBittorrent.
ALTER TABLE torrents ADD COLUMN content_layout TEXT NOT NULL DEFAULT 'Original';

-- +goose Down
ALTER TABLE torrents DROP COLUMN content_layout;
ALTER TABLE torrents DROP COLUMN file_paths;
ALTER TABLE torrents DROP COLUMN web_seeds;
ALTER TABLE torrents DROP COLUMN trackers;
