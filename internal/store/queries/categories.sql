-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: UpsertCategory :exec
INSERT INTO categories (name, save_path) VALUES (sqlc.arg(name), sqlc.arg(save_path))
ON CONFLICT (name) DO UPDATE SET save_path = excluded.save_path;

-- name: DeleteCategory :execrows
DELETE FROM categories WHERE name = sqlc.arg(name);

-- name: ListTags :many
SELECT name FROM tags ORDER BY name;

-- name: CreateTag :exec
INSERT INTO tags (name) VALUES (sqlc.arg(name)) ON CONFLICT DO NOTHING;

-- name: DeleteTag :execrows
DELETE FROM tags WHERE name = sqlc.arg(name);
