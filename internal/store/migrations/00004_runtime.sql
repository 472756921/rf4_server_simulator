-- 运行时表：网关会话凭据、跨进程指令队列与共享 KV。
CREATE TABLE IF NOT EXISTS gw_token(
    token      TEXT PRIMARY KEY,
    info_json  TEXT NOT NULL,
    expires    REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS gw_handoff(
    token      TEXT PRIMARY KEY,
    state_json TEXT NOT NULL,
    saved_at   REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS kv(
    k  TEXT PRIMARY KEY,
    v  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS admin_queue(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    payload    TEXT NOT NULL,
    created_ts REAL NOT NULL
);
