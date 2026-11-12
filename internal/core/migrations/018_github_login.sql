-- Fence in-flight upstream authentication when local credentials or status change.
ALTER TABLE identities ADD COLUMN authentication_version bigint NOT NULL DEFAULT 1;
CREATE FUNCTION bump_authentication_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.password_hash,NEW.mfa_secret,NEW.status,NEW.email,NEW.role,NEW.deleted_at)
 IS DISTINCT FROM ROW(OLD.password_hash,OLD.mfa_secret,OLD.status,OLD.email,OLD.role,OLD.deleted_at) THEN
  NEW.authentication_version := OLD.authentication_version+1;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER identities_authentication_version BEFORE UPDATE ON identities
 FOR EACH ROW EXECUTE FUNCTION bump_authentication_version();
CREATE TABLE github_accounts (
 github_id bigint PRIMARY KEY CHECK(github_id>0),
 identity_id text NOT NULL UNIQUE REFERENCES identities(id),
 login text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE github_flows (
 state_hash text PRIMARY KEY, browser_hash text NOT NULL,
 purpose text NOT NULL CHECK(purpose IN ('login','register','link','mfa')),
 verifier text NOT NULL DEFAULT '', session_id text NOT NULL DEFAULT '',
 subject text NOT NULL DEFAULT '', subject_version bigint NOT NULL DEFAULT 0, auth_request_id text NOT NULL DEFAULT '',
 device_code text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes'
);
CREATE INDEX github_flows_expiry ON github_flows(expires_at);
