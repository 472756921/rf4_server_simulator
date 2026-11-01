-- Static-dataset provenance. The hash covers the complete canonical
-- 21-table snapshot.
CREATE TABLE IF NOT EXISTS static_dataset_meta(
    name           TEXT PRIMARY KEY,
    schema_version INTEGER NOT NULL,
    data_version   TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    table_count    INTEGER NOT NULL,
    row_count      INTEGER NOT NULL,
    applied_at     REAL NOT NULL
);

-- catch_log's table-level UNIQUE treats every NULL as distinct.  Keep the most
-- recent row per business key before adding the NULL-safe expression index.
DELETE FROM catch_log
WHERE id NOT IN (
    SELECT MAX(id)
    FROM catch_log
    GROUP BY source, fish_key, weight_g,
             loc IS NULL, COALESCE(loc, ''), spot IS NULL, COALESCE(spot, 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_catch_log_business_key
ON catch_log(
    source, fish_key, weight_g,
    loc IS NULL, COALESCE(loc, ''), spot IS NULL, COALESCE(spot, 0)
);
CREATE INDEX IF NOT EXISTS idx_catch_log_uid_fish_weight
ON catch_log(uid, fish_key, weight_g);

-- rig_component.guid is one physical item and may belong to only one rig.
-- Remove rows pointing at a missing rig first, then reconcile duplicates with
-- item.rig_guid as the authoritative parent when that parent is valid.
DELETE FROM rig_component
WHERE guid IS NOT NULL AND guid <> ''
  AND NOT EXISTS (SELECT 1 FROM rig WHERE rig.guid = rig_component.rig_guid);

DELETE FROM rig_component
WHERE guid IS NOT NULL AND guid <> ''
  AND EXISTS (
      SELECT 1
      FROM item i
      JOIN rig target ON target.guid = i.rig_guid AND target.uid = i.uid
      WHERE i.guid = rig_component.guid
        AND i.rig_guid <> rig_component.rig_guid
  )
  AND EXISTS (
      SELECT 1
      FROM item i
      JOIN rig_component keep
        ON keep.guid = i.guid AND keep.rig_guid = i.rig_guid
      WHERE i.guid = rig_component.guid
  );

-- If the authoritative parent has no component row yet and the slot is free,
-- move the stale row to the parent.  If the slot is occupied, the stale row is
-- removed below because keeping it would violate the one-parent invariant.
UPDATE rig_component
SET rig_guid = (
    SELECT i.rig_guid
    FROM item i
    JOIN rig target ON target.guid = i.rig_guid AND target.uid = i.uid
    WHERE i.guid = rig_component.guid
)
WHERE guid IS NOT NULL AND guid <> ''
  AND EXISTS (
      SELECT 1
      FROM item i
      JOIN rig target ON target.guid = i.rig_guid AND target.uid = i.uid
      WHERE i.guid = rig_component.guid
        AND i.rig_guid <> rig_component.rig_guid
  )
  AND NOT EXISTS (
      SELECT 1
      FROM item i
      JOIN rig_component existing
        ON existing.rig_guid = i.rig_guid
       AND existing.slot = rig_component.slot
       AND existing.ordinal = rig_component.ordinal
      WHERE i.guid = rig_component.guid
  );

DELETE FROM rig_component
WHERE guid IS NOT NULL AND guid <> ''
  AND EXISTS (
      SELECT 1
      FROM item i
      WHERE i.guid = rig_component.guid
        AND i.rig_guid IS NOT NULL AND i.rig_guid <> ''
        AND i.rig_guid <> rig_component.rig_guid
  );

-- Last-resort deterministic dedup for rows without a usable item parent.
WITH ranked AS (
    SELECT rc.rowid AS rid,
           ROW_NUMBER() OVER (
               PARTITION BY rc.guid
               ORDER BY
                   CASE WHEN i.rig_guid = rc.rig_guid THEN 0 ELSE 1 END,
                   CASE WHEN r.uid = i.uid THEN 0 ELSE 1 END,
                   rc.rig_guid, rc.slot, rc.rowid DESC
           ) AS rn
    FROM rig_component rc
    LEFT JOIN item i ON i.guid = rc.guid
    LEFT JOIN rig r ON r.guid = rc.rig_guid
    WHERE rc.guid IS NOT NULL AND rc.guid <> ''
)
DELETE FROM rig_component
WHERE rowid IN (SELECT rid FROM ranked WHERE rn > 1);

CREATE UNIQUE INDEX IF NOT EXISTS uq_rig_component_guid
ON rig_component(guid)
WHERE guid IS NOT NULL AND guid <> '';

-- A physical item can occupy at most one shortcut/container position.
DELETE FROM container_slot
WHERE guid IS NOT NULL AND guid <> ''
  AND rowid NOT IN (
      SELECT MAX(rowid) FROM container_slot
      WHERE guid IS NOT NULL AND guid <> ''
      GROUP BY guid
  );
CREATE UNIQUE INDEX IF NOT EXISTS uq_container_slot_guid
ON container_slot(guid)
WHERE guid IS NOT NULL AND guid <> '';
