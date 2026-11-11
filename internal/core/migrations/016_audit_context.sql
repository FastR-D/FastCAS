ALTER TABLE audit_events ADD COLUMN request_id text NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN result text NOT NULL DEFAULT 'unknown'
 CHECK (result IN ('unknown','success','failure'));
CREATE INDEX audit_events_request_id ON audit_events(request_id) WHERE request_id <> '';
