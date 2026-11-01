-- rig_component 使用 (rig_guid, slot, ordinal) 表示同一品类的多个实例。
-- 迁移行统一写入 ordinal=0；后续同品类实例使用 1,2,...。
-- slot 仍表示 item category，不编码复合值。
ALTER TABLE rig_component RENAME TO rig_component_legacy_00007;

CREATE TABLE rig_component(
    rig_guid TEXT    NOT NULL,
    slot     INTEGER NOT NULL,
    ordinal  INTEGER NOT NULL DEFAULT 0,
    guid     TEXT,
    extra    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(rig_guid, slot, ordinal)
);

INSERT INTO rig_component(rig_guid, slot, ordinal, guid, extra)
SELECT rig_guid, slot, 0, guid, extra FROM rig_component_legacy_00007;

DROP TABLE rig_component_legacy_00007;

CREATE INDEX IF NOT EXISTS idx_rig_component_guid ON rig_component(guid);
