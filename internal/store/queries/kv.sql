-- name: ListKV :many
SELECT key, value FROM kv WHERE namespace = sqlc.arg(namespace) ORDER BY key;

-- name: PutKV :exec
INSERT INTO kv (namespace, key, value) VALUES (sqlc.arg(namespace), sqlc.arg(key), sqlc.arg(value))
ON CONFLICT (namespace, key) DO UPDATE SET value = excluded.value;

-- name: DeleteKV :exec
DELETE FROM kv WHERE namespace = sqlc.arg(namespace) AND key = sqlc.arg(key);

-- name: DeleteKVNamespace :exec
DELETE FROM kv WHERE namespace = sqlc.arg(namespace);
