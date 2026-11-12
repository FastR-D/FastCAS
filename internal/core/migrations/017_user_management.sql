ALTER TABLE identities ADD COLUMN deleted_at timestamptz;
CREATE INDEX identities_visible ON identities(id) WHERE deleted_at IS NULL;
