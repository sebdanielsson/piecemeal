-- name: GetUser :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = sqlc.arg(username);

-- name: GetUserByOIDC :one
SELECT * FROM users WHERE oidc_issuer = sqlc.arg(issuer) AND oidc_subject = sqlc.arg(subject);

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: CreateUser :one
INSERT INTO users (username, display_name, email, oidc_issuer, oidc_subject, is_admin, created_at)
VALUES (sqlc.arg(username), sqlc.arg(display_name), sqlc.arg(email), sqlc.narg(oidc_issuer), sqlc.narg(oidc_subject), sqlc.arg(is_admin), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateUserProfile :exec
UPDATE users SET display_name = sqlc.arg(display_name), email = sqlc.arg(email), is_admin = sqlc.arg(is_admin)
WHERE id = sqlc.arg(id);

-- name: SetUserLastLogin :exec
UPDATE users SET last_login_at = sqlc.arg(at) WHERE id = sqlc.arg(id);

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = sqlc.arg(id);
