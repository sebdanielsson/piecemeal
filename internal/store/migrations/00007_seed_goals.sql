-- +goose Up

-- What happens when a torrent reaches a seeding goal, and whether it takes
-- any goal or all of them. 'default' follows the torrent's category, then
-- the global setting.
-- seed_goal_action: default, stop, remove or removeWithData.
ALTER TABLE torrents ADD COLUMN seed_goal_action TEXT NOT NULL DEFAULT 'default';
-- seed_goal_mode: default, any or all.
ALTER TABLE torrents ADD COLUMN seed_goal_mode TEXT NOT NULL DEFAULT 'default';

-- Seeding goals per category, for its torrents that follow the default.
-- -2 and 'default' follow the global setting, -1 is unlimited.
ALTER TABLE categories ADD COLUMN ratio_limit REAL NOT NULL DEFAULT -2;
ALTER TABLE categories ADD COLUMN seeding_time_limit INTEGER NOT NULL DEFAULT -2;
ALTER TABLE categories ADD COLUMN inactive_seeding_time_limit INTEGER NOT NULL DEFAULT -2;
ALTER TABLE categories ADD COLUMN seed_goal_action TEXT NOT NULL DEFAULT 'default';
ALTER TABLE categories ADD COLUMN seed_goal_mode TEXT NOT NULL DEFAULT 'default';

-- +goose Down

ALTER TABLE categories DROP COLUMN seed_goal_mode;
ALTER TABLE categories DROP COLUMN seed_goal_action;
ALTER TABLE categories DROP COLUMN inactive_seeding_time_limit;
ALTER TABLE categories DROP COLUMN seeding_time_limit;
ALTER TABLE categories DROP COLUMN ratio_limit;
ALTER TABLE torrents DROP COLUMN seed_goal_mode;
ALTER TABLE torrents DROP COLUMN seed_goal_action;
