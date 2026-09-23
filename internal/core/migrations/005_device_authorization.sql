CREATE TABLE device_authorizations (
 device_code_hash text PRIMARY KEY,
 user_code text NOT NULL UNIQUE,
 client_id text NOT NULL REFERENCES applications(id),
 scopes jsonb NOT NULL,
 expires_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','denied','consumed')),
 subject text REFERENCES identities(id),
 auth_time timestamptz,
 next_poll_at timestamptz NOT NULL DEFAULT now(),
 poll_interval_seconds integer NOT NULL DEFAULT 5,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX device_authorizations_expiry ON device_authorizations(expires_at);
