CREATE TABLE reusable_invites (
 token_hash text PRIMARY KEY,
 allow_register boolean NOT NULL,
 allow_claim boolean NOT NULL,
 remaining_uses integer NOT NULL CHECK (remaining_uses BETWEEN 0 AND 10000),
 expires_at timestamptz NOT NULL,
 CHECK (allow_register OR allow_claim)
);

ALTER TABLE one_time_credentials DROP CONSTRAINT one_time_credentials_kind_check;
ALTER TABLE one_time_credentials ADD CONSTRAINT one_time_credentials_kind_check
 CHECK (kind IN ('invite','recovery','mfa_reset','claim','claim_proof'));
ALTER TABLE one_time_credentials DROP CONSTRAINT one_time_credentials_claim_target_check;
ALTER TABLE one_time_credentials ADD CONSTRAINT one_time_credentials_claim_target_check
 CHECK ((kind IN ('claim','claim_proof')) = (identity_id IS NOT NULL));
DROP INDEX one_time_credentials_open_claim;
CREATE UNIQUE INDEX one_time_credentials_open_claim
 ON one_time_credentials(identity_id) WHERE kind IN ('claim','claim_proof') AND consumed_at IS NULL;
