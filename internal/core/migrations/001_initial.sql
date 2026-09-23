CREATE TABLE identities (
 id text PRIMARY KEY, email text NOT NULL UNIQUE, name text NOT NULL,
 password_hash text NOT NULL, role text NOT NULL DEFAULT 'member' CHECK(role IN ('member','admin')),
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 email_verified boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE browser_sessions (
 id text PRIMARY KEY, token_hash text NOT NULL UNIQUE, identity_id text NOT NULL REFERENCES identities(id),
 csrf_hash text NOT NULL, authenticated_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL, revoked_at timestamptz
);
CREATE TABLE applications (
 id text PRIMARY KEY, name text NOT NULL, secret_hash text NOT NULL,
 config jsonb NOT NULL, active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_requests (
 id text PRIMARY KEY, client_id text NOT NULL REFERENCES applications(id), payload jsonb NOT NULL,
 code_hash text UNIQUE, expires_at timestamptz NOT NULL, code_expires_at timestamptz, consumed_at timestamptz
);
CREATE TABLE token_families (
 id text PRIMARY KEY, subject text NOT NULL, client_id text NOT NULL REFERENCES applications(id),
 session_id text, revoked_at timestamptz, expires_at timestamptz NOT NULL
);
CREATE TABLE refresh_tokens (
 token_hash text PRIMARY KEY, family_id text NOT NULL REFERENCES token_families(id),
 payload jsonb NOT NULL, consumed_at timestamptz, expires_at timestamptz NOT NULL
);
CREATE TABLE access_tokens (
 id text PRIMARY KEY, client_id text NOT NULL REFERENCES applications(id), subject text NOT NULL,
 family_id text REFERENCES token_families(id), payload jsonb NOT NULL,
 expires_at timestamptz NOT NULL, revoked_at timestamptz
);
CREATE TABLE link_intents (
 id text PRIMARY KEY, client_id text NOT NULL REFERENCES applications(id), local_ref text NOT NULL,
 idempotency_key text NOT NULL, payload_hash text NOT NULL,
 nonce text NOT NULL, subject text REFERENCES identities(id),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','prepared','active','revoked')),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(client_id,idempotency_key)
);
CREATE TABLE account_links (
 id text PRIMARY KEY REFERENCES link_intents(id), client_id text NOT NULL REFERENCES applications(id),
 local_ref text NOT NULL, subject text NOT NULL REFERENCES identities(id),
 state text NOT NULL CHECK(state IN ('prepared','active','revoked')), version bigint NOT NULL DEFAULT 1,
 verified_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz
);
CREATE UNIQUE INDEX links_local_live ON account_links(client_id,local_ref) WHERE state <> 'revoked';
CREATE UNIQUE INDEX links_subject_live ON account_links(client_id,subject) WHERE state <> 'revoked';
CREATE TABLE one_time_credentials (
 token_hash text PRIMARY KEY, kind text NOT NULL CHECK(kind IN ('invite','recovery')),
 email text NOT NULL, expires_at timestamptz NOT NULL, consumed_at timestamptz
);
CREATE TABLE audit_events (
 id bigserial PRIMARY KEY, actor text NOT NULL, action text NOT NULL, target text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE outbox (
 id text PRIMARY KEY, client_id text NOT NULL REFERENCES applications(id), payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz, delivered_at timestamptz, attempts integer NOT NULL DEFAULT 0
);
CREATE TABLE rate_limits (
 key text PRIMARY KEY, count integer NOT NULL, expires_at timestamptz NOT NULL
);
CREATE INDEX access_subject ON access_tokens(subject);
CREATE INDEX family_subject ON token_families(subject);
CREATE INDEX outbox_pending ON outbox(available_at) WHERE delivered_at IS NULL;
