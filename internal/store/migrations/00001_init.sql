-- +goose Up

-- Runtime settings changed through the API or UI, one JSON value per key.
-- Values pinned by environment variables are never written here.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    display_name  TEXT    NOT NULL DEFAULT '',
    email         TEXT    NOT NULL DEFAULT '',
    oidc_issuer   TEXT,
    oidc_subject  TEXT,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER,
    UNIQUE (oidc_issuer, oidc_subject)
) STRICT;

-- Sessions for the web UI and for qBittorrent clients (SID cookie). Only a
-- SHA-256 hash of the token is stored.
CREATE TABLE sessions (
    token_hash   BLOB    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    ip           TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX sessions_user_id ON sessions (user_id);

CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    prefix       TEXT    NOT NULL,
    key_hash     BLOB    NOT NULL UNIQUE,
    scope        TEXT    NOT NULL,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
) STRICT;

CREATE TABLE categories (
    name      TEXT PRIMARY KEY,
    save_path TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE tags (
    name TEXT PRIMARY KEY
) STRICT;

-- Everything about a torrent that anacrolix does not persist itself. Piece
-- completion lives in anacrolix's bolt database next to this one.
CREATE TABLE torrents (
    info_hash                   TEXT    PRIMARY KEY,
    name                        TEXT    NOT NULL,
    magnet                      TEXT    NOT NULL DEFAULT '',
    metainfo                    BLOB,
    save_path                   TEXT    NOT NULL,
    category                    TEXT    NOT NULL DEFAULT '',
    added_at                    INTEGER NOT NULL,
    completed_at                INTEGER NOT NULL DEFAULT 0,
    stopped                     INTEGER NOT NULL DEFAULT 0,
    forced                      INTEGER NOT NULL DEFAULT 0,
    sequential                  INTEGER NOT NULL DEFAULT 0,
    first_last_piece            INTEGER NOT NULL DEFAULT 0,
    queue_position              INTEGER NOT NULL DEFAULT 0,
    uploaded                    INTEGER NOT NULL DEFAULT 0,
    downloaded                  INTEGER NOT NULL DEFAULT 0,
    seeding_seconds             INTEGER NOT NULL DEFAULT 0,
    active_seconds              INTEGER NOT NULL DEFAULT 0,
    last_activity_at            INTEGER NOT NULL DEFAULT 0,
    -- Seed goals: -2 follows the global setting, -1 is unlimited.
    ratio_limit                 REAL    NOT NULL DEFAULT -2,
    seeding_time_limit          INTEGER NOT NULL DEFAULT -2,
    inactive_seeding_time_limit INTEGER NOT NULL DEFAULT -2,
    -- Bytes per second, 0 is unlimited.
    download_limit              INTEGER NOT NULL DEFAULT 0,
    upload_limit                INTEGER NOT NULL DEFAULT 0,
    -- JSON array of per-file priorities, empty until changed.
    file_priorities             TEXT    NOT NULL DEFAULT '',
    error                       TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE torrent_tags (
    info_hash TEXT NOT NULL REFERENCES torrents (info_hash) ON DELETE CASCADE,
    tag       TEXT NOT NULL REFERENCES tags (name) ON DELETE CASCADE,
    PRIMARY KEY (info_hash, tag)
) STRICT;

CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY,
    at     INTEGER NOT NULL,
    actor  TEXT    NOT NULL,
    action TEXT    NOT NULL,
    detail TEXT    NOT NULL DEFAULT ''
) STRICT;

-- +goose Down
DROP TABLE audit_log;
DROP TABLE torrent_tags;
DROP TABLE torrents;
DROP TABLE tags;
DROP TABLE categories;
DROP TABLE api_keys;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE settings;
