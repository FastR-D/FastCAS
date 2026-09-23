#!/usr/bin/env python3
"""Build the implemented FastCAS binding and administration OpenAPI contract.

The provider's OAuth/OIDC endpoints are described by discovery, not by this file.
Run this script after changing an API handler, then run check.py.
"""

import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def ref(name):
    return {"$ref": f"#/components/schemas/{name}"}


def obj(properties, required=(), description=None):
    result = {"type": "object", "properties": properties, "additionalProperties": False}
    if required:
        result["required"] = list(required)
    if description:
        result["description"] = description
    return result


def string(**more):
    return {"type": "string", **more}


def integer(**more):
    return {"type": "integer", **more}


def array(items):
    return {"type": "array", "items": items}


S = {
    "StringList": array(string()),
    "NullableStringList": {"type": ["array", "null"], "items": string()},
    "NonEmptyStringList": {"type": "array", "items": string(minLength=1), "minItems": 1},
    "Error": obj({"code": string(), "message": string(), "request_id": string(), "retryable": {"type": "boolean"}}, ("code",)),
    "LinkIntent": obj({"id": string(), "client_id": string(), "local_account_ref": string(), "nonce": string(), "state": string(), "expires_at": string(format="date-time")}, ("id", "client_id", "local_account_ref", "nonce", "state", "expires_at")),
    "AccountLink": obj({"id": string(), "client_id": string(), "local_account_ref": string(), "subject": string(), "state": string(), "version": integer(format="int64"), "verified_at": string(format="date-time")}, ("id", "client_id", "local_account_ref", "subject", "state", "version", "verified_at")),
    "Identity": obj({"id": string(), "email": string(format="email"), "name": string(), "role": string(enum=["member", "admin"]), "status": string(enum=["active", "disabled"]), "email_verified": {"type": "boolean"}}, ("id", "email", "name", "role", "status", "email_verified")),
    "Client": obj({"id": string(), "name": string(), "redirect_uris": ref("NullableStringList"), "post_logout_redirect_uris": ref("NullableStringList"), "scopes": ref("NullableStringList"), "resources": ref("NullableStringList"), "grant_types": ref("NullableStringList"), "public": {"type": "boolean"}, "development": {"type": "boolean"}, "backchannel_logout_uri": string(format="uri"), "events_uri": string(format="uri")}, ("id", "name", "redirect_uris", "post_logout_redirect_uris", "scopes", "resources", "grant_types", "public", "development")),
    "ServiceAccount": obj({"client": ref("Client"), "active": {"type": "boolean"}, "created_at": string(format="date-time")}, ("client", "active", "created_at")),
    "ExchangePolicy": obj({"caller_client": string(), "target_client": string(), "resource": string(), "scope": string(), "active": {"type": "boolean"}}, ("caller_client", "target_client", "resource", "scope", "active")),
    "AuditEvent": obj({"id": integer(format="int64"), "actor": string(), "action": string(), "target": string(), "created_at": string(format="date-time")}, ("id", "actor", "action", "target", "created_at")),
    "OutboxEvent": obj({"id": string(), "client_id": string(), "kind": string(), "attempts": integer(), "last_status": {"type": ["integer", "null"]}, "created_at": string(format="date-time"), "available_at": string(format="date-time"), "dead_at": {"type": ["string", "null"], "format": "date-time"}, "delivered_at": {"type": ["string", "null"], "format": "date-time"}}, ("id", "client_id", "kind", "attempts", "created_at", "available_at")),
    "OutboxAttempt": obj({"id": integer(format="int64"), "attempt": integer(), "outcome": string(enum=["delivered", "http_error", "transport_error", "configuration_error", "signing_error"]), "http_status": {"type": ["integer", "null"]}, "duration_ms": integer(minimum=0), "finished_at": string(format="date-time")}, ("id", "attempt", "outcome", "duration_ms", "finished_at")),
    "BrowserSession": obj({"id": string(), "identity_id": string(), "authenticated_at": string(format="date-time"), "expires_at": string(format="date-time")}, ("id", "identity_id", "authenticated_at", "expires_at")),
    "DeviceInstallation": obj({"id": string(), "client_id": string(), "client_name": string(), "installation_id": string(format="uuid"), "created_at": string(format="date-time"), "revoked_at": {"type": ["string", "null"], "format": "date-time"}}, ("id", "client_id", "client_name", "installation_id", "created_at", "revoked_at")),
    "RegisterDeviceInstallation": obj({"installation_id": string(format="uuid")}, ("installation_id",)),
    "RegisteredDeviceInstallation": obj({"installation": ref("DeviceInstallation"), "management_secret": string()}, ("installation", "management_secret"), "The management secret is returned once and only authorizes reading/revoking this installation."),
    "DeviceInstallationPage": obj({"installations": array(ref("DeviceInstallation")), "next_cursor": string()}, ("installations", "next_cursor")),
    "Me": obj({"user": ref("Identity"), "session": ref("BrowserSession"), "csrf": string(), "mfa_enabled": {"type": "boolean"}}, ("user", "session", "csrf", "mfa_enabled")),
    "RedeemCredential": obj({"token": string(), "name": string(), "password": string()}, ("token", "password")),
    "RedeemedCredential": obj({"user": ref("Identity"), "login_required": {"type": "boolean"}}, ("user", "login_required")),
    "RedeemMFAReset": obj({"token": string(), "password": string()}, ("token", "password")),
    "RedeemedMFAReset": obj({"login_required": {"type": "boolean"}}, ("login_required",)),
    "MFAConfirm": obj({"code": string()}, ("code",)),
    "MFAEnroll": obj({"otpauth_uri": string(format="uri")}, ("otpauth_uri",)),
    "MFARecovery": obj({"recovery_codes": ref("StringList")}, ("recovery_codes",)),
    "Reauthenticate": obj({"password": string(), "code": string()}, ("password",)),
    "ChangePassword": obj({"current_password": string(), "new_password": string(minLength=12, maxLength=256), "code": string()}, ("current_password", "new_password")),
    "Version": obj({"version": integer(format="int64")}, ("version",)),
    "CreateLinkIntent": obj({"local_account_ref": string(minLength=1, maxLength=256)}, ("local_account_ref",)),
    "PrepareLink": obj({"nonce": string(), "subject": string()}, ("nonce", "subject")),
    "IdentityStatus": obj({"status": string(enum=["active", "disabled"])}, ("status",)),
    "IssueCredential": obj({"kind": string(enum=["invite", "recovery", "mfa_reset"]), "email": string(format="email")}, ("kind", "email")),
    "CreateServiceAccount": obj({"id": string(), "name": string(), "scopes": ref("NonEmptyStringList"), "resources": ref("NonEmptyStringList"), "development": {"type": "boolean", "default": False}}, ("id", "name", "scopes", "resources")),
    "CreateApplication": obj({"id": string(), "name": string(), "redirect_uris": ref("StringList"), "post_logout_redirect_uris": ref("StringList"), "scopes": ref("StringList"), "resources": ref("StringList"), "grant_types": ref("NonEmptyStringList"), "public": {"type": "boolean"}, "development": {"type": "boolean"}, "backchannel_logout_uri": string(format="uri"), "events_uri": string(format="uri")}, ("id", "name", "grant_types", "public"), "Redirect URIs are required for authorization_code; service-only clients need nonempty scopes and resources and should use /admin/service-accounts."),
    "ServiceStatus": obj({"active": {"type": "boolean"}}, ("active",)),
    "RotateSecret": obj({"grace_seconds": integer(minimum=0, maximum=3600, default=600)}),
    "ApplicationCallbacks": obj({"events_uri": string(), "backchannel_logout_uri": string()}),
    "ApplicationCapabilities": obj({"grant_types": ref("StringList"), "scopes": ref("StringList"), "resources": ref("StringList")}, ("grant_types", "scopes", "resources")),
}

S["OneTimeSecret"] = obj({"client_secret": string()}, ("client_secret",), "The credential is returned once and never included in list responses.")
S["CreatedServiceAccount"] = obj({"id": string(), "client_secret": string()}, ("id", "client_secret"))
S["ServiceStatusResult"] = obj({"active": {"type": "boolean"}, "client_secret": string()}, ("active", "client_secret"))
S["RotatedSecret"] = obj({"client_secret": string(), "previous_valid_until_seconds": integer()}, ("client_secret", "previous_valid_until_seconds"))
S["CreatedApplication"] = obj({"client": ref("Client"), "client_secret": string()}, ("client", "client_secret"))
S["CreatedCredential"] = obj({"token": string(), "delivery": string()}, ("token", "delivery"))
S["OutboxPage"] = obj({"events": array(ref("OutboxEvent")), "next_cursor": string()}, ("events", "next_cursor"))
S["OutboxAttempts"] = obj({"attempts": array(ref("OutboxAttempt"))}, ("attempts",))
S["Queued"] = obj({"status": string(enum=["queued"])}, ("status",))


def response(schema, status=200):
    if status == 204:
        return {"204": {"description": "Completed; no response body"}}
    return {str(status): {"description": "Success", "content": {"application/json": {"schema": schema}}}}


def request(schema):
    return {"required": True, "content": {"application/json": {"schema": schema}}}


PATHS = {}
ERRORS = {str(code): {"description": label, "content": {"application/json": {"schema": ref("Error")}}} for code, label in ((400, "Invalid or expired request"), (401, "Authentication required"), (403, "Forbidden or recent MFA required"), (409, "Conflict"), (500, "Server error"))}
PARAMS = {
    "id": {"name": "id", "in": "path", "required": True, "schema": string()},
    "origin": {"name": "Origin", "in": "header", "required": True, "schema": string(format="uri"), "description": "Must exactly match the configured FastCAS issuer."},
    "csrf": {"name": "X-CSRF-Token", "in": "header", "required": True, "schema": string(), "description": "Token from the browser's authenticated /api/v1/me response."},
    "after": {"name": "after", "in": "query", "schema": string(), "description": "Return records with an ID greater than this cursor; maximum 100 per page."},
}


def add(path, method, operation, title, security, result, *, body=None, params=(), description=""):
    tags = {"clientBasic": "Binding", "adminSession": "Administration", "browserSession": "Security center", "deviceAccess": "Device installations", "installationSecret": "Device installations", "public": "Public account"}
    item = {"operationId": operation, "summary": title, "tags": [tags[security]], "security": [] if security == "public" else [{security: []}], "responses": {**result, **ERRORS}}
    if description:
        item["description"] = description
    if body is not None:
        item["requestBody"] = request(ref(body))
    parameters = [PARAMS[p] if isinstance(p, str) and p in PARAMS else p for p in params]
    if security in ("adminSession", "browserSession") and method == "post":
        parameters = [PARAMS["origin"], PARAMS["csrf"], *parameters]
    if security == "public" and method == "post":
        parameters = [PARAMS["origin"], *parameters]
    if parameters:
        item["parameters"] = parameters
    PATHS.setdefault(path, {})[method] = item


basic = "clientBasic"
admin = "adminSession"
browser = "browserSession"
public = "public"
link = response(ref("AccountLink"))
add("/api/v1/link-intents", "post", "createLinkIntent", "Create a project-account binding intent", basic, response(ref("LinkIntent"), 201), body="CreateLinkIntent", params=({"name": "Idempotency-Key", "in": "header", "required": True, "schema": string(minLength=16, maxLength=128)},))
add("/api/v1/link-intents/{id}/prepare", "post", "prepareLink", "Prepare a confirmed binding", basic, link, body="PrepareLink", params=("id",))
add("/api/v1/link-intents/{id}/activate", "post", "activateLink", "Activate a locally committed binding", basic, link, params=("id",))
add("/api/v1/account-links/resolve", "get", "resolveLink", "Resolve this application's active binding", basic, link, params=({"name": "subject", "in": "query", "required": True, "schema": string()},))
add("/api/v1/account-links/{id}", "get", "getLink", "Read this application's binding", basic, link, params=("id",))
add("/api/v1/account-links/{id}/revoke", "post", "revokeLink", "Revoke a binding at the expected version", basic, link, params=("id", {"name": "If-Match", "in": "header", "required": True, "schema": string(), "description": "Decimal link version, optionally wrapped in quotes."}))
PATHS["/api/v1/account-links/{id}/revoke"]["post"]["responses"]["428"] = {"description": "If-Match version required", "content": {"application/json": {"schema": ref("Error")}}}

add("/api/v1/register", "post", "redeemInvitation", "Redeem a FastCAS invitation", public, response(ref("RedeemedCredential")), body="RedeemCredential")
add("/api/v1/recover", "post", "redeemRecovery", "Redeem a FastCAS password-recovery credential", public, response(ref("RedeemedCredential")), body="RedeemCredential")
add("/api/v1/recover-mfa", "post", "redeemMFAReset", "Redeem an administrator-issued MFA reset credential with the current FastCAS password", public, response(ref("RedeemedMFAReset")), body="RedeemMFAReset")
for path in ("/api/v1/register", "/api/v1/recover", "/api/v1/recover-mfa"):
    PATHS[path]["post"]["responses"]["429"] = {"description": "Redeem rate limit", "content": {"application/json": {"schema": ref("Error")}}}
add("/api/v1/me", "get", "getMe", "Get the current FastCAS identity and CSRF token", browser, response(ref("Me")))
add("/api/v1/logout", "post", "logout", "Log out this browser session", browser, response(None, 204))
add("/api/v1/me/mfa/enroll", "post", "enrollMFA", "Start authenticator enrollment", browser, response(ref("MFAEnroll")))
add("/api/v1/me/mfa/confirm", "post", "confirmMFA", "Confirm authenticator enrollment", browser, response(ref("MFARecovery")), body="MFAConfirm")
add("/api/v1/me/mfa/recovery-codes", "post", "rotateMFARecoveryCodes", "Replace all one-time MFA recovery codes after recent password and MFA verification", browser, response(ref("MFARecovery")))
add("/api/v1/me/reauthenticate", "post", "reauthenticate", "Refresh recent authentication", browser, response(None, 204), body="Reauthenticate")
add("/api/v1/me/password", "post", "changePassword", "Change the FastCAS password with the current password and MFA when enabled; revoke FastCAS sessions and authorizations", browser, response(None, 204), body="ChangePassword")
add("/api/v1/me/links", "get", "listMyLinks", "List my FastCAS account bindings", browser, response(array(ref("AccountLink"))))
add("/api/v1/me/links/{id}/revoke", "post", "revokeMyLink", "Revoke my binding after recent authentication", browser, link, body="Version", params=("id",))
add("/api/v1/device-installations", "post", "registerDeviceInstallation", "Register one local installation after approved device authorization", "deviceAccess", response(ref("RegisteredDeviceInstallation"), 201), body="RegisterDeviceInstallation")
add("/api/v1/device-installations/{id}", "get", "getDeviceInstallation", "Read this local installation status", "installationSecret", response(ref("DeviceInstallation")), params=("id",))
add("/api/v1/device-installations/{id}/revoke", "post", "revokeDeviceInstallation", "Revoke this local installation", "installationSecret", response(None, 204), params=("id",))
add("/api/v1/me/device-installations", "get", "listMyDeviceInstallations", "List my local installations", browser, response(ref("DeviceInstallationPage")), params=({"name": "before", "in": "query", "schema": string(), "description": "Cursor from the previous page; up to 50 installations per page."},))
add("/api/v1/me/device-installations/{id}/revoke", "post", "revokeMyDeviceInstallation", "Revoke my local installation after recent authentication", browser, response(None, 204), params=("id",))
add("/api/v1/me/sessions", "get", "listMySessions", "List my active FastCAS browser sessions", browser, response(array(ref("BrowserSession"))))
add("/api/v1/me/sessions/{id}/revoke", "post", "revokeMySession", "Revoke one FastCAS browser session", browser, response(None, 204), params=("id",))
add("/api/v1/me/logout-all", "post", "logoutAll", "Revoke all FastCAS browser sessions", browser, response(None, 204))
add("/api/v1/me/delegations", "get", "listMyDelegations", "List available cross-project delegation options", browser, response(array(ref("ExchangePolicy"))))
add("/api/v1/me/delegations", "post", "setMyDelegation", "Set my cross-project delegation consent", browser, response(None, 204), body="ExchangePolicy")

add("/api/v1/admin/identities", "get", "listIdentities", "List FastCAS identities", admin, response(array(ref("Identity"))), params=("after",))
add("/api/v1/admin/identities/{id}/status", "post", "setIdentityStatus", "Enable or disable a FastCAS identity", admin, response(None, 204), body="IdentityStatus", params=("id",))
add("/api/v1/admin/credentials", "post", "issueCredential", "Issue a one-time invitation, password recovery, or MFA reset credential", admin, response(ref("CreatedCredential"), 201), body="IssueCredential")
add("/api/v1/admin/applications", "get", "listApplications", "List registered applications", admin, response(array(ref("Client"))), params=("after",))
add("/api/v1/admin/applications", "post", "createApplication", "Register an application", admin, response(ref("CreatedApplication"), 201), body="CreateApplication")
add("/api/v1/admin/applications/{id}/callbacks", "post", "setApplicationCallbacks", "Set application event and logout callbacks", admin, response(ref("Client")), body="ApplicationCallbacks", params=("id",))
add("/api/v1/admin/applications/{id}/capabilities", "post", "setApplicationCapabilities", "Set application grants, scopes, and resources", admin, response(ref("Client")), body="ApplicationCapabilities", params=("id",))
add("/api/v1/admin/applications/{id}/rotate-secret", "post", "rotateApplicationSecret", "Rotate an application credential", admin, response(ref("RotatedSecret")), body="RotateSecret", params=("id",))
add("/api/v1/admin/service-accounts", "get", "listServiceAccounts", "List service identities", admin, response(array(ref("ServiceAccount"))), params=("after",))
add("/api/v1/admin/service-accounts", "post", "createServiceAccount", "Register a client-credentials-only service identity", admin, response(ref("CreatedServiceAccount"), 201), body="CreateServiceAccount")
add("/api/v1/admin/service-accounts/{id}/status", "post", "setServiceAccountStatus", "Disable or re-enable a service identity", admin, response(ref("ServiceStatusResult")), body="ServiceStatus", params=("id",), description="Disabling revokes centrally tracked tokens and all old secrets. Re-enabling issues a fresh secret once. Offline JWT verification can accept old tokens until expiration.")
add("/api/v1/admin/service-accounts/{id}/rotate-secret", "post", "rotateServiceAccountSecret", "Rotate a service credential", admin, response(ref("RotatedSecret")), body="RotateSecret", params=("id",))
add("/api/v1/admin/exchange-policies", "get", "listExchangePolicies", "List token-exchange routes", admin, response(array(ref("ExchangePolicy"))))
add("/api/v1/admin/exchange-policies", "post", "setExchangePolicy", "Set a token-exchange route", admin, response(None, 204), body="ExchangePolicy")
add("/api/v1/admin/audit-events", "get", "listAuditEvents", "List audit events, newest first", admin, response(array(ref("AuditEvent"))), params=({"name": "before", "in": "query", "schema": integer(minimum=1), "description": "Return events with IDs lower than this cursor."},))
add("/api/v1/admin/outbox", "get", "listOutbox", "List outbox delivery status", admin, response(ref("OutboxPage")), params=({"name": "before", "in": "query", "schema": string()}, {"name": "state", "in": "query", "schema": string(enum=["dead", "all"]), "description": "Omitting state returns dead-letter items."}))
add("/api/v1/admin/outbox/{id}/attempts", "get", "listOutboxAttempts", "List up to 100 recent delivery attempts without event payloads or signed tokens", admin, response(ref("OutboxAttempts")), params=("id",))
add("/api/v1/admin/outbox/{id}/retry", "post", "retryOutbox", "Retry a dead-letter event", admin, response(ref("Queued")), params=("id",))

document = {
    "openapi": "3.1.0",
    "info": {"title": "FastCAS binding and administration API", "version": "0.1.0", "description": "Implemented JSON API. OAuth/OIDC protocol endpoints are obtained from issuer discovery. Application accounts and local login remain independent and optional."},
    "paths": PATHS,
    "components": {"securitySchemes": {"clientBasic": {"type": "http", "scheme": "basic", "description": "Confidential application ID and secret. Never use in a browser."}, "deviceAccess": {"type": "http", "scheme": "bearer", "description": "Short-lived access token obtained through an approved device-code grant."}, "installationSecret": {"type": "http", "scheme": "bearer", "description": "Installation-scoped management secret stored only on the local installation."}, "adminSession": {"type": "apiKey", "in": "cookie", "name": "fastcas_session", "description": "Administrator browser session with recent MFA. POST additionally needs exact Origin and X-CSRF-Token."}, "browserSession": {"type": "apiKey", "in": "cookie", "name": "fastcas_session", "description": "Ordinary FastCAS browser session. POST additionally needs exact Origin and X-CSRF-Token."}}, "schemas": S},
}

output = json.dumps(document, ensure_ascii=False, indent=2) + "\n"
target = HERE / "openapi.json"
if "--check" in sys.argv:
    if not target.exists() or target.read_text(encoding="utf-8") != output:
        raise SystemExit("openapi.json is stale; run python3 api/generate.py")
else:
    target.write_text(output, encoding="utf-8")
