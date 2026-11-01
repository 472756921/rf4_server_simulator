-- 14/5 keepnet must retain the capture map so each fish renders with the map
-- from capture time.
ALTER TABLE keepnet_items ADD COLUMN keep_level TEXT;

-- catch_log records retained fish only; released fish must not be counted.
DELETE FROM catch_log WHERE source='catch';
