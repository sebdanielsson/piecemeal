-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, kind, created_at, last_seen_at, expires_at, ip, user_agent)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(created_at), sqlc.arg(created_at), sqlc.arg(expires_at), sqlc.arg(ip), sqlc.arg(user_agent));

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = sqlc.arg(at) WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteSessionForUser :execrows
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash) AND user_id = sqlc.arg(user_id);

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id);

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now) OR last_seen_at <= sqlc.arg(idle_before);

-- name: ListSessions :many
SELECT * FROM sessions ORDER BY last_seen_at DESC;

-- name: ListUserSessions :many
SELECT * FROM sessions WHERE user_id = sqlc.arg(user_id) ORDER BY last_seen_at DESC;
