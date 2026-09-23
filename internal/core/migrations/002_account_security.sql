ALTER TABLE identities ADD COLUMN mfa_secret bytea;
ALTER TABLE identities ADD COLUMN pending_mfa_secret bytea;
ALTER TABLE identities ADD COLUMN pending_mfa_expires timestamptz;
ALTER TABLE identities ADD COLUMN mfa_last_step bigint NOT NULL DEFAULT -1;
ALTER TABLE browser_sessions ADD COLUMN mfa_at timestamptz;
CREATE TABLE recovery_codes (
 identity_id text NOT NULL REFERENCES identities(id), token_hash text NOT NULL,
 consumed_at timestamptz, PRIMARY KEY(identity_id,token_hash)
);
