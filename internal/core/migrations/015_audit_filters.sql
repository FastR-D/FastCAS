CREATE INDEX audit_events_action_page ON audit_events(action,id DESC);
CREATE INDEX audit_events_actor_page ON audit_events(actor,id DESC);
CREATE INDEX audit_events_target_page ON audit_events(target,id DESC);
CREATE INDEX audit_events_created_at ON audit_events(created_at);
