CREATE TABLE outbox_attempts (
 id bigserial PRIMARY KEY,
 event_id text NOT NULL REFERENCES outbox(id) ON DELETE CASCADE,
 attempt integer NOT NULL,
 outcome text NOT NULL CHECK (outcome IN ('delivered','http_error','transport_error','configuration_error','signing_error')),
 http_status integer,
 duration_ms integer NOT NULL CHECK (duration_ms >= 0),
 finished_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_attempts_event ON outbox_attempts(event_id,id DESC);
