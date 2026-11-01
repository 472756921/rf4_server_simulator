-- 玩家上次所在位置。profile 是玩家主档；登录时 1/13 从这里恢复，
-- 不能只依赖 Server.levelMap（进程重启即丢）。
ALTER TABLE profile ADD COLUMN last_level_key TEXT NOT NULL DEFAULT '';
ALTER TABLE profile ADD COLUMN last_scene TEXT NOT NULL DEFAULT '';
ALTER TABLE profile ADD COLUMN last_spot INTEGER NOT NULL DEFAULT 0;
ALTER TABLE profile ADD COLUMN location_updated_ts REAL NOT NULL DEFAULT 0;
