CREATE TABLE fastresearch_imports (
 local_account_id text PRIMARY KEY,
 identity_id text NOT NULL UNIQUE REFERENCES identities(id),
 legacy_name text NOT NULL CHECK (btrim(legacy_name) <> ''),
 claimed_at timestamptz
);

ALTER TABLE one_time_credentials DROP CONSTRAINT one_time_credentials_kind_check;
ALTER TABLE one_time_credentials ADD CONSTRAINT one_time_credentials_kind_check
 CHECK (kind IN ('invite','recovery','mfa_reset','claim'));
ALTER TABLE one_time_credentials ADD COLUMN identity_id text REFERENCES fastresearch_imports(identity_id);
ALTER TABLE one_time_credentials ADD CONSTRAINT one_time_credentials_claim_target_check
 CHECK ((kind='claim') = (identity_id IS NOT NULL));
CREATE UNIQUE INDEX one_time_credentials_open_claim
 ON one_time_credentials(identity_id) WHERE kind='claim' AND consumed_at IS NULL;
