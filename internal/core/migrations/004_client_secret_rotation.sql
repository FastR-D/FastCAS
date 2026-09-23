ALTER TABLE applications ADD COLUMN previous_secret_hash text;
ALTER TABLE applications ADD COLUMN previous_secret_expires_at timestamptz;
