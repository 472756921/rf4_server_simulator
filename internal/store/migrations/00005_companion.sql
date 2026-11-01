-- companion 饵效表；静态配置库在 seed 时复制，缺表时按空数据处理。
-- 迁移器按整文件执行，不解析 Down 段；不要添加 -- +goose Down。
CREATE TABLE IF NOT EXISTS companion_bait_fish(
    bait_id TEXT NOT NULL, fish_key TEXT NOT NULL,
    tier INTEGER NOT NULL, evidence INTEGER NOT NULL,
    PRIMARY KEY(bait_id, fish_key));
CREATE TABLE IF NOT EXISTS companion_fish_meta(
    fish_key TEXT PRIMARY KEY,
    total_evidence INTEGER NOT NULL, n_baits INTEGER NOT NULL);
