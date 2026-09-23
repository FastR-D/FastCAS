# FastCAS API contract

[`openapi.json`](openapi.json) is the OpenAPI 3.1 contract for all implemented `/api/v1/` JSON endpoints: public invitation/recovery, browser security center, project binding and administration. Standard OIDC/OAuth URLs, methods and metadata come from `/.well-known/openid-configuration`; SDKs should use discovery for them instead of fixing those paths from this file.

The binding endpoints use confidential-client HTTP Basic and are called by the project server. Browser security-center and administrator endpoints use the `fastcas_session` cookie; their writes require an exact `Origin` matching the issuer and `X-CSRF-Token` from `GET /api/v1/me`. Administrator access also requires recent MFA. Public invitation/recovery writes require an exact Origin but no session. Do not place client secrets in a browser or generate management privileges into ordinary project SDKs.

After changing an `/api/v1/` handler, run `python3 api/generate.py` and `python3 api/check.py`. The checker verifies the generated file is current, all implemented JSON routes are listed, all path parameters and schema references resolve, operation IDs are unique, and write operations document their Origin/CSRF boundary. It does not replace actual HTTP tests or assert every field of a response; protocol behavior remains governed by the service implementation.

Administrators with recent MFA can list dead-letter events at `GET /api/v1/admin/outbox` and read up to 100 recent metadata-only attempts for one event at `GET /api/v1/admin/outbox/{id}/attempts`. Attempt records include outcome, HTTP status and elapsed time, but never the event payload, signed token, endpoint URL or response body. Manual dead-letter retry preserves the original event ID and its earlier attempt records.
