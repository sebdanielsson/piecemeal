-- name: CreateAPIKey :one
INSERT INTO api_keys (name, prefix, key_hash, scope, user_id, created_at)
VALUES (sqlc.arg(name), sqlc.arg(prefix), sqlc.arg(key_hash), sqlc.arg(scope), sqlc.arg(user_id), sqlc.arg(created_at))
RETURNING *;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = sqlc.arg(key_hash);

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = sqlc.arg(at) WHERE id = sqlc.arg(id);

-- name: ListAPIKeys :many
SELECT * FROM api_keys ORDER BY created_at DESC;

-- name: DeleteAPIKey :execrows
DELETE FROM api_keys WHERE id = sqlc.arg(id);
