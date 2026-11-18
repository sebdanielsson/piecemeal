-- +goose Up

-- Small values that don't need a table of their own, grouped by namespace:
-- the qBittorrent API's clientdata store and cookie jar.
CREATE TABLE kv (
    namespace TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (namespace, key)
) STRICT, WITHOUT ROWID;

-- +goose Down
DROP TABLE kv;
