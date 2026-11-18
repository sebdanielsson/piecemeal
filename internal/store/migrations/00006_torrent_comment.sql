-- +goose Up

-- comment replaces the torrent file's comment when set (qBittorrent's
-- torrents/setComment); NULL shows the torrent file's.
ALTER TABLE torrents ADD COLUMN comment TEXT;

-- +goose Down
ALTER TABLE torrents DROP COLUMN comment;
