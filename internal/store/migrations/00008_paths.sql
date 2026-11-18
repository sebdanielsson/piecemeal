-- +goose Up

-- The incomplete folder: where a torrent's data stays until it completes,
-- then it moves to save_path. '' keeps it in save_path all along.
ALTER TABLE torrents ADD COLUMN incomplete_path TEXT NOT NULL DEFAULT '';
-- in_incomplete_path: the data is in incomplete_path now, rather than in
-- save_path. Cleared once it has moved to save_path.
ALTER TABLE torrents ADD COLUMN in_incomplete_path INTEGER NOT NULL DEFAULT 0;
-- auto_managed: automatic torrent management. The save path and the
-- incomplete folder follow the torrent's category.
ALTER TABLE torrents ADD COLUMN auto_managed INTEGER NOT NULL DEFAULT 0;

-- A category's incomplete folder. incomplete_path_mode is 'default' to
-- follow the global setting, 'off' for none, or 'on' for incomplete_path.
ALTER TABLE categories ADD COLUMN incomplete_path_mode TEXT NOT NULL DEFAULT 'default';
ALTER TABLE categories ADD COLUMN incomplete_path TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE categories DROP COLUMN incomplete_path;
ALTER TABLE categories DROP COLUMN incomplete_path_mode;
ALTER TABLE torrents DROP COLUMN auto_managed;
ALTER TABLE torrents DROP COLUMN in_incomplete_path;
ALTER TABLE torrents DROP COLUMN incomplete_path;
