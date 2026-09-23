-- Installations revoked before token revocation was coupled to the row may
-- still have unexpired device access tokens. Close that upgrade window.
UPDATE access_tokens a
SET revoked_at = COALESCE(a.revoked_at, d.revoked_at)
FROM device_installations d
WHERE a.id = d.device_token_id
  AND d.revoked_at IS NOT NULL
  AND a.revoked_at IS NULL;
