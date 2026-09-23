ALTER TABLE one_time_credentials DROP CONSTRAINT one_time_credentials_kind_check;
ALTER TABLE one_time_credentials ADD CONSTRAINT one_time_credentials_kind_check
 CHECK(kind IN ('invite','recovery','mfa_reset'));
