-- 鱼市挂单订单簿。
-- 一条记录只对应一条护网鱼；partial unique index 保证同一时刻只能有一个 open 挂单。
CREATE TABLE IF NOT EXISTS market_offer(
    offer_id     TEXT PRIMARY KEY,
    market_guid  TEXT NOT NULL DEFAULT '',
    fish_guid    TEXT NOT NULL,
    seller_uid   INTEGER NOT NULL,
    price_cents  INTEGER NOT NULL CHECK(price_cents >= 0),
    status       TEXT NOT NULL CHECK(status IN ('open', 'filled', 'cancelled')),
    buyer_uid    INTEGER,
    created_ts   REAL NOT NULL,
    updated_ts   REAL NOT NULL,
    filled_ts    REAL,
    cancelled_ts REAL
);

-- 取消/成交后允许该鱼重新挂单，但任何时刻最多一个 open。
CREATE UNIQUE INDEX IF NOT EXISTS uq_market_offer_open_fish
    ON market_offer(fish_guid) WHERE status='open';
CREATE INDEX IF NOT EXISTS idx_market_offer_seller_status
    ON market_offer(seller_uid, status, created_ts);
CREATE INDEX IF NOT EXISTS idx_market_offer_market_status
    ON market_offer(market_guid, status, created_ts);
CREATE INDEX IF NOT EXISTS idx_market_offer_fish
    ON market_offer(fish_guid, created_ts);

-- 挂单只能绑定真实护网行；此约束也覆盖并发插入。
CREATE TRIGGER IF NOT EXISTS trg_market_offer_requires_keepnet
BEFORE INSERT ON market_offer
WHEN NOT EXISTS (
    SELECT 1 FROM keepnet_items WHERE echo_guid=NEW.fish_guid
)
BEGIN
    SELECT RAISE(ABORT, 'market_offer fish missing');
END;

-- 护网删除（20/2 卖鱼、14/6 放生等）不得绕过 open 挂单锁。
CREATE TRIGGER IF NOT EXISTS trg_market_offer_blocks_keepnet_delete
BEFORE DELETE ON keepnet_items
WHEN EXISTS (
    SELECT 1 FROM market_offer
    WHERE fish_guid=OLD.echo_guid AND status='open'
)
BEGIN
    SELECT RAISE(ABORT, 'market_offer fish locked');
END;

-- 挂单期间不得把鱼改到其他 UID；成交时先置 filled，再在同一事务内转移。
CREATE TRIGGER IF NOT EXISTS trg_market_offer_blocks_keepnet_transfer
BEFORE UPDATE OF uid ON keepnet_items
WHEN NEW.uid <> OLD.uid AND EXISTS (
    SELECT 1 FROM market_offer
    WHERE fish_guid=OLD.echo_guid AND status='open'
)
BEGIN
    SELECT RAISE(ABORT, 'market_offer fish locked');
END;
