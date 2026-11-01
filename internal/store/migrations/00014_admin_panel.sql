-- 管理后台可写覆盖层。静态 shop_item / loc_fish 仍保持只读；
-- 运行时读取静态值并优先采用这里的后台覆盖。
CREATE TABLE IF NOT EXISTS shop_item_override(
    store_id    TEXT    NOT NULL,
    item_id     TEXT    NOT NULL,
    silver      INTEGER,
    gold        INTEGER,
    stock       INTEGER,
    updated_ts  REAL    NOT NULL DEFAULT 0,
    updated_by  TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY(store_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_shop_item_override_item
    ON shop_item_override(item_id);

CREATE TABLE IF NOT EXISTS fish_weight_override(
    loc         TEXT    NOT NULL,
    fish_key    TEXT    NOT NULL,
    weight      REAL    NOT NULL DEFAULT 1,
    enabled     INTEGER NOT NULL DEFAULT 1,
    updated_ts  REAL    NOT NULL DEFAULT 0,
    updated_by  TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY(loc, fish_key)
);
CREATE INDEX IF NOT EXISTS idx_fish_weight_override_loc
    ON fish_weight_override(loc);

CREATE TABLE IF NOT EXISTS fish_bite_setting(
    loc                 TEXT PRIMARY KEY,
    bite_probability    REAL,
    bite_delay_fixed    REAL,
    bite_delay_scale    REAL,
    bite_delay_max      REAL,
    bite_delay_lure_max REAL,
    updated_ts          REAL NOT NULL DEFAULT 0,
    updated_by          TEXT NOT NULL DEFAULT ''
);
