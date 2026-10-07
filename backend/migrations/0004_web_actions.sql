ALTER TABLE jobs ADD COLUMN action_ready BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE plugin_metadata SET version=4 WHERE id=1;
