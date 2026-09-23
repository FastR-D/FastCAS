CREATE TABLE device_installations (
 id text PRIMARY KEY,
 client_id text NOT NULL REFERENCES applications(id),
 subject text NOT NULL REFERENCES identities(id),
 installation_ref uuid NOT NULL,
 device_token_id text NOT NULL UNIQUE,
 secret_hash text NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now(),
 revoked_at timestamptz
);
CREATE UNIQUE INDEX device_installations_live_ref ON device_installations(client_id,installation_ref) WHERE revoked_at IS NULL;
CREATE INDEX device_installations_subject ON device_installations(subject,created_at DESC);
