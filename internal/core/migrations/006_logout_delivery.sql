ALTER TABLE outbox ADD COLUMN kind text NOT NULL DEFAULT 'event' CHECK(kind IN ('event','logout'));
