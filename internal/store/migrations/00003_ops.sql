-- 运营/管理后台表 + 常用索引
CREATE TABLE IF NOT EXISTS tunables(
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_ts REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS admin_user(
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    username     TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_ts   REAL NOT NULL DEFAULT 0,
    last_login_ts REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS audit_log(
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    admin  TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    ts     REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS gift_pack(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    items_json TEXT NOT NULL DEFAULT '[]',
    created_by TEXT NOT NULL DEFAULT '',
    created_ts REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_item_uid_container ON item(uid, container);
CREATE INDEX IF NOT EXISTS idx_txn_uid ON txn(uid);
CREATE INDEX IF NOT EXISTS idx_mail_uid ON mail(uid);
CREATE INDEX IF NOT EXISTS idx_catch_log_uid ON catch_log(uid);
CREATE INDEX IF NOT EXISTS idx_catch_log_fish ON catch_log(fish_key);
CREATE INDEX IF NOT EXISTS idx_rig_component_rig ON rig_component(rig_guid);
CREATE INDEX IF NOT EXISTS idx_device_uid ON device(uid);
