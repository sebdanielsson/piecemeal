-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

-- name: UpsertCategory :exec
INSERT INTO categories (
    name, save_path, ratio_limit, seeding_time_limit, inactive_seeding_time_limit, seed_goal_action, seed_goal_mode,
    incomplete_path_mode, incomplete_path
) VALUES (
    sqlc.arg(name), sqlc.arg(save_path), sqlc.arg(ratio_limit), sqlc.arg(seeding_time_limit),
    sqlc.arg(inactive_seeding_time_limit), sqlc.arg(seed_goal_action), sqlc.arg(seed_goal_mode),
    sqlc.arg(incomplete_path_mode), sqlc.arg(incomplete_path)
)
ON CONFLICT (name) DO UPDATE SET
    save_path = excluded.save_path,
    ratio_limit = excluded.ratio_limit,
    seeding_time_limit = excluded.seeding_time_limit,
    inactive_seeding_time_limit = excluded.inactive_seeding_time_limit,
    seed_goal_action = excluded.seed_goal_action,
    seed_goal_mode = excluded.seed_goal_mode,
    incomplete_path_mode = excluded.incomplete_path_mode,
    incomplete_path = excluded.incomplete_path;

-- name: DeleteCategory :execrows
DELETE FROM categories WHERE name = sqlc.arg(name);

-- name: ListTags :many
SELECT name FROM tags ORDER BY name;

-- name: CreateTag :exec
INSERT INTO tags (name) VALUES (sqlc.arg(name)) ON CONFLICT DO NOTHING;

-- name: DeleteTag :execrows
DELETE FROM tags WHERE name = sqlc.arg(name);

-- name: CreateCategoryIfMissing :exec
INSERT INTO categories (name, save_path) VALUES (sqlc.arg(name), '') ON CONFLICT (name) DO NOTHING;
