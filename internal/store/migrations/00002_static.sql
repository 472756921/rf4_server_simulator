-- 静态资料表；seed 从静态库复制行，表结构保持一致。
CREATE TABLE IF NOT EXISTS item_def(
    numeric_id INTEGER PRIMARY KEY,
    item_id    TEXT,
    category   INTEGER,
    kind       TEXT,
    brand      TEXT,
    attrs_json TEXT
);
CREATE TABLE IF NOT EXISTS item_catalog(
    item_id  TEXT PRIMARY KEY,
    name     TEXT,
    name_zh  TEXT,
    kind     TEXT,
    numeric_id INTEGER
);
CREATE TABLE IF NOT EXISTS item_price(
    item_id TEXT PRIMARY KEY,
    silver  INTEGER NOT NULL DEFAULT 0,
    gold    INTEGER NOT NULL DEFAULT 0,
    stock   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS recipe(
    recipe_id TEXT PRIMARY KEY,
    name      TEXT,
    output_id TEXT,
    output_qty INTEGER NOT NULL DEFAULT 1,
    materials_json TEXT,
    duration_sec REAL,
    kind      TEXT
);
CREATE TABLE IF NOT EXISTS skill_def(
    cat_id     INTEGER NOT NULL,
    node_id    INTEGER NOT NULL,
    unlock_pct REAL    NOT NULL DEFAULT 0,
    max_level  INTEGER NOT NULL DEFAULT 0,
    effect_pct REAL    NOT NULL DEFAULT 0,
    group_id   INTEGER NOT NULL DEFAULT 0,
    enabled    INTEGER NOT NULL DEFAULT 1,
    ordinal    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(cat_id, node_id)
);
CREATE TABLE IF NOT EXISTS skill_cat_def(
    cat_id       INTEGER PRIMARY KEY,
    ord          INTEGER NOT NULL DEFAULT 0,
    title_key    TEXT,
    category_kind INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS spot_def(
    system_id   TEXT PRIMARY KEY,
    map_key     TEXT,
    kind        INTEGER,
    fish_roster TEXT
);
CREATE TABLE IF NOT EXISTS shop_item(
    store_id TEXT    NOT NULL,
    item_id  TEXT    NOT NULL,
    silver   INTEGER NOT NULL DEFAULT 0,
    gold     INTEGER NOT NULL DEFAULT 0,
    stock    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(store_id, item_id)
);
CREATE TABLE IF NOT EXISTS species_weight(fish_key TEXT PRIMARY KEY, min_g INTEGER,
                                    max_g INTEGER, trophy_g INTEGER, src TEXT,
                                    length_k REAL, length_p REAL, xp_c REAL, xp_q REAL,
                                    trophy_line_g INTEGER, super_trophy_line_g INTEGER, avg_g INTEGER, samples INTEGER);
CREATE TABLE IF NOT EXISTS species_grade(fish_key TEXT NOT NULL, catches INTEGER NOT NULL,
                                      trophy INTEGER DEFAULT 0, super INTEGER DEFAULT 0,
                                      PRIMARY KEY(fish_key));
CREATE TABLE IF NOT EXISTS loc_fish(loc TEXT NOT NULL, fish_key TEXT NOT NULL,
                              name_zh TEXT, catches INTEGER NOT NULL,
                              PRIMARY KEY(loc, fish_key));
CREATE TABLE IF NOT EXISTS loc_alias(alias TEXT PRIMARY KEY, loc TEXT NOT NULL, note TEXT);
CREATE TABLE IF NOT EXISTS loc_bait(loc TEXT NOT NULL, bait TEXT NOT NULL,
                                 catches INTEGER NOT NULL,
                                 PRIMARY KEY(loc, bait));
CREATE TABLE IF NOT EXISTS loc_bait_fish(loc TEXT NOT NULL, bait TEXT NOT NULL,
                                      fish_key TEXT NOT NULL, catches INTEGER NOT NULL,
                                      trophy INTEGER DEFAULT 0, super INTEGER DEFAULT 0,
                                      PRIMARY KEY(loc, bait, fish_key));
CREATE TABLE IF NOT EXISTS loc_wiki_fish(loc TEXT NOT NULL, fish_key TEXT NOT NULL, fish_zh TEXT,
                                      in_map_roster INTEGER DEFAULT 1, in_habitat INTEGER DEFAULT 0,
                                      PRIMARY KEY(loc, fish_key));
CREATE TABLE IF NOT EXISTS spot_fish(loc TEXT NOT NULL, spot TEXT NOT NULL,
                               fish_key TEXT NOT NULL, catches INTEGER NOT NULL,
                               PRIMARY KEY(loc, spot, fish_key));
CREATE TABLE IF NOT EXISTS fish_bait_pref(fish_key TEXT NOT NULL, bait_zh TEXT NOT NULL,
                                      PRIMARY KEY(fish_key, bait_zh));
CREATE TABLE IF NOT EXISTS sonar_profile(system_id TEXT PRIMARY KEY, cells INTEGER NOT NULL,
                                      depth_min REAL, depth_max REAL,
                                      size_med REAL, size_p90 REAL);
CREATE TABLE IF NOT EXISTS map_fish_roster(loc TEXT NOT NULL, fish_key TEXT NOT NULL, source TEXT DEFAULT 'rf4db',
                                      PRIMARY KEY(loc, fish_key));
