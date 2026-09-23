ALTER TABLE outbox ADD COLUMN lease_token text;
ALTER TABLE outbox ADD COLUMN last_status integer;
ALTER TABLE outbox ADD COLUMN dead_at timestamptz;
