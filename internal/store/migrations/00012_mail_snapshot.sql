-- Mail attachment snapshots are stored inside items_json. Every attachment
-- keeps guid/item_id/qty readable; snapshot_version distinguishes full-instance
-- snapshots from entries that contain only those base fields.
ALTER TABLE mail ADD COLUMN attachment_snapshot_version INTEGER NOT NULL DEFAULT 0;