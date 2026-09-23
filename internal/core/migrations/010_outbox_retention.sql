CREATE INDEX outbox_delivered_retention ON outbox(delivered_at,id) WHERE delivered_at IS NOT NULL;
