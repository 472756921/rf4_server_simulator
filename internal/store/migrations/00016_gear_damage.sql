-- 渔具损伤历史：保留每次磨损、断线、断竿、报废与修理事件。
CREATE TABLE IF NOT EXISTS item_damage_log (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    uid               INTEGER NOT NULL,
    item_guid         TEXT NOT NULL,
    item_id           TEXT,
    event             TEXT NOT NULL,
    before_durability REAL,
    after_durability  REAL,
    before_length     INTEGER,
    after_length      INTEGER,
    broken_guid       TEXT,
    detail_json       TEXT,
    created_ts        REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_item_damage_log_uid_ts
    ON item_damage_log(uid, created_ts DESC);
CREATE INDEX IF NOT EXISTS idx_item_damage_log_item_ts
    ON item_damage_log(item_guid, created_ts DESC);
