CREATE TABLE authorization_consents (
    subject text NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    scopes text[] NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject, client_id)
);
