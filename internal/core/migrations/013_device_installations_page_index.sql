DROP INDEX device_installations_subject;
CREATE INDEX device_installations_subject ON device_installations(subject,created_at DESC,id DESC);
