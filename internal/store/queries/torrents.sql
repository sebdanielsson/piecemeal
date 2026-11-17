-- name: ListTorrents :many
SELECT * FROM torrents ORDER BY added_at, info_hash;

-- name: UpsertTorrent :exec
INSERT INTO torrents (
    info_hash, name, magnet, metainfo, save_path, category, added_at, completed_at, stopped, forced,
    sequential, first_last_piece, queue_position, uploaded, downloaded, seeding_seconds, active_seconds,
    last_activity_at, ratio_limit, seeding_time_limit, inactive_seeding_time_limit, download_limit,
    upload_limit, file_priorities, error
) VALUES (
    sqlc.arg(info_hash), sqlc.arg(name), sqlc.arg(magnet), sqlc.arg(metainfo), sqlc.arg(save_path),
    sqlc.arg(category), sqlc.arg(added_at), sqlc.arg(completed_at), sqlc.arg(stopped), sqlc.arg(forced),
    sqlc.arg(sequential), sqlc.arg(first_last_piece), sqlc.arg(queue_position), sqlc.arg(uploaded),
    sqlc.arg(downloaded), sqlc.arg(seeding_seconds), sqlc.arg(active_seconds), sqlc.arg(last_activity_at),
    sqlc.arg(ratio_limit), sqlc.arg(seeding_time_limit), sqlc.arg(inactive_seeding_time_limit),
    sqlc.arg(download_limit), sqlc.arg(upload_limit), sqlc.arg(file_priorities), sqlc.arg(error)
)
ON CONFLICT (info_hash) DO UPDATE SET
    name = excluded.name,
    magnet = excluded.magnet,
    metainfo = excluded.metainfo,
    save_path = excluded.save_path,
    category = excluded.category,
    completed_at = excluded.completed_at,
    stopped = excluded.stopped,
    forced = excluded.forced,
    sequential = excluded.sequential,
    first_last_piece = excluded.first_last_piece,
    queue_position = excluded.queue_position,
    uploaded = excluded.uploaded,
    downloaded = excluded.downloaded,
    seeding_seconds = excluded.seeding_seconds,
    active_seconds = excluded.active_seconds,
    last_activity_at = excluded.last_activity_at,
    ratio_limit = excluded.ratio_limit,
    seeding_time_limit = excluded.seeding_time_limit,
    inactive_seeding_time_limit = excluded.inactive_seeding_time_limit,
    download_limit = excluded.download_limit,
    upload_limit = excluded.upload_limit,
    file_priorities = excluded.file_priorities,
    error = excluded.error;

-- name: DeleteTorrent :exec
DELETE FROM torrents WHERE info_hash = sqlc.arg(info_hash);

-- name: ClearCategory :exec
UPDATE torrents SET category = '' WHERE category = sqlc.arg(category);

-- name: ListTorrentTags :many
SELECT info_hash, tag FROM torrent_tags ORDER BY info_hash, tag;

-- name: AddTorrentTag :exec
INSERT INTO torrent_tags (info_hash, tag) VALUES (sqlc.arg(info_hash), sqlc.arg(tag)) ON CONFLICT DO NOTHING;

-- name: RemoveTorrentTag :exec
DELETE FROM torrent_tags WHERE info_hash = sqlc.arg(info_hash) AND tag = sqlc.arg(tag);

-- name: InsertAudit :exec
INSERT INTO audit_log (at, actor, action, detail) VALUES (sqlc.arg(at), sqlc.arg(actor), sqlc.arg(action), sqlc.arg(detail));

-- name: ListAudit :many
SELECT * FROM audit_log WHERE id < sqlc.arg(before) ORDER BY id DESC LIMIT sqlc.arg(limit);
