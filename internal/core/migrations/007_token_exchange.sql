CREATE TABLE exchange_policies (
 caller_client text NOT NULL REFERENCES applications(id),
 target_client text NOT NULL REFERENCES applications(id),
 resource text NOT NULL,
 scope text NOT NULL,
 active boolean NOT NULL DEFAULT true,
 PRIMARY KEY(caller_client,target_client,resource,scope),
 UNIQUE(caller_client,resource,scope)
);
CREATE TABLE exchange_consents (
 subject text NOT NULL REFERENCES identities(id),
 caller_client text NOT NULL REFERENCES applications(id),
 target_client text NOT NULL REFERENCES applications(id),
 resource text NOT NULL,
 scope text NOT NULL,
 granted_at timestamptz NOT NULL DEFAULT now(),
 revoked_at timestamptz,
 PRIMARY KEY(subject,caller_client,target_client,resource,scope)
);
