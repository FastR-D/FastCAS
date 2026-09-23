from __future__ import annotations

import asyncio
import base64
import hashlib
import hmac
import secrets
import threading
import time
from dataclasses import dataclass
from typing import Awaitable, Callable
from urllib.parse import urlencode, urlparse, parse_qs, quote

import httpx
from authlib.jose import JsonWebToken
from authlib.oidc.core import CodeIDToken

from .store import TransactionStore


class FastCASError(Exception):
    def __init__(self, code: str, message: str, status: int = 400):
        super().__init__(message)
        self.code, self.status = code, status


@dataclass(frozen=True)
class Configuration:
    issuer: str
    client_id: str
    redirect_uri: str
    client_secret: str = ""
    allow_loopback_http: bool = False
    timeout: float = 10


def _hash(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def _url(value: str, dev: bool):
    u = urlparse(value)
    if not u.netloc or u.username or u.password or u.fragment:
        raise FastCASError("invalid_url", "Invalid endpoint URL")
    if u.scheme != "https" and not (dev and u.scheme == "http" and u.hostname in {"localhost", "127.0.0.1", "::1"}):
        raise FastCASError("https_required", "HTTPS required outside explicit loopback development")
    return u


class FastCAS:
    def __init__(self, configuration: Configuration, transactions: TransactionStore, *, http: httpx.Client | None = None):
        self.config, self.transactions = configuration, transactions
        issuer = _url(configuration.issuer, configuration.allow_loopback_http)
        if issuer.path or issuer.query or not configuration.client_id:
            raise FastCASError("invalid_configuration", "Issuer origin and client ID required")
        _url(configuration.redirect_uri, configuration.allow_loopback_http)
        self.http = http or httpx.Client(timeout=configuration.timeout, follow_redirects=False)
        self._owns_http = http is None
        self._metadata: dict | None = None
        self._keys: dict | None = None
        self._keys_at = 0.0
        self._keys_lock = threading.Lock()
        self._jwt = JsonWebToken(["RS256"])

    def reset_verification_cache(self):
        """Trusted operations only, after draining in-flight authentication.

        This does not revoke existing application sessions.
        """
        with self._keys_lock:
            self._keys = None
            self._keys_at = 0.0
            self._metadata = None

    def close(self):
        if self._owns_http:
            self.http.close()

    def _json(self, response: httpx.Response) -> dict:
        if not response.is_success:
            try:
                error = response.json()
            except ValueError:
                error = {}
            raise FastCASError(error.get("code") or error.get("error") or "request_failed", error.get("message") or "FastCAS request failed", response.status_code)
        result = response.json()
        if not isinstance(result, dict):
            raise FastCASError("invalid_response", "Expected an object response")
        return result

    def discovery(self) -> dict:
        if self._metadata is None:
            metadata = self._json(self.http.get(self.config.issuer + "/.well-known/openid-configuration"))
            if metadata.get("issuer") != self.config.issuer:
                raise FastCASError("issuer_mismatch", "Unexpected discovery issuer")
            for field in ("authorization_endpoint", "token_endpoint", "jwks_uri", "userinfo_endpoint"):
                endpoint = _url(metadata.get(field, ""), self.config.allow_loopback_http)
                if f"{endpoint.scheme}://{endpoint.netloc}" != self.config.issuer:
                    raise FastCASError("endpoint_mismatch", "FastCAS endpoints must belong to the configured issuer")
            self._metadata = metadata
        return dict(self._metadata)

    def _jwks(self, force=False) -> dict:
        with self._keys_lock:
            age = time.monotonic() - self._keys_at
            if self._keys is None or age > 300 or (force and age > 30):
                self._keys = self._json(self.http.get(self.discovery()["jwks_uri"]))
                self._keys_at = time.monotonic()
            return self._keys

    def _claims(self, token: str, audience: str, *, nonce: str | None = None, access_token: str = "", event: bool = False, logout: bool = False) -> dict:
        options = {"iss": {"essential": True, "value": self.config.issuer}, "aud": {"essential": True, "value": audience}, "sub": {"essential": True}, "iat": {"essential": True}, "exp": {"essential": True}}
        if event:
            options.pop("sub")
            options["jti"] = {"essential": True}
        kwargs = {"claims_options": options}
        if nonce is not None:
            kwargs.update(claims_cls=CodeIDToken, claims_params={"client_id": audience, "nonce": nonce, "access_token": access_token})
        try:
            # Only the configured JWKS is used. JWT jku/x5u never selects a URL.
            try:
                claims = self._jwt.decode(token, self._jwks(), **kwargs)
            except (ValueError, KeyError):
                claims = self._jwt.decode(token, self._jwks(force=True), **kwargs)
            claims.validate(leeway=60)
            if event and claims.header.get("typ") != "fastcas-event+jwt":
                raise ValueError("invalid event type")
            if logout and claims.header.get("typ") != "logout+jwt":
                raise ValueError("invalid logout type")
            if nonce is not None and not hmac.compare_digest(str(claims.get("nonce", "")), nonce):
                raise ValueError("nonce mismatch")
            return dict(claims)
        except Exception as exc:
            if isinstance(exc, (httpx.HTTPError, FastCASError)):
                raise
            raise FastCASError("invalid_token", "Token validation failed", 401) from None

    def begin_login(self, browser_binding: str, *, return_to: str = "/", scopes: list[str] | None = None) -> str:
        return self._begin("login", browser_binding, return_to, scopes)

    def begin_link(self, browser_binding: str, *, local_account_ref: str, local_session_id: str, return_to: str = "/", scopes: list[str] | None = None) -> str:
        if not local_account_ref or not local_session_id:
            raise FastCASError("local_proof_required", "Recent local account and session proof required")
        return self._begin("link", browser_binding, return_to, scopes, local_account_ref, local_session_id)

    def _begin(self, purpose, binding, return_to, scopes, local_ref="", session="") -> str:
        if len(binding) < 32:
            raise FastCASError("binding_required", "Fresh browser binding required")
        if not return_to.startswith("/") or return_to.startswith("//") or any(c in return_to for c in "\\\r\n"):
            raise FastCASError("invalid_return_to", "Application-relative return path required")
        endpoint = self.discovery()["authorization_endpoint"]
        tx = dict(state=secrets.token_urlsafe(32), nonce=secrets.token_urlsafe(32), verifier=secrets.token_urlsafe(32), binding_hash=_hash(binding), purpose=purpose, return_to=return_to, expires_at=time.time()+300)
        if purpose in {"link", "register"}:
            tx.update(local_account_ref=local_ref, local_session_id=session)
            intent = self._api("POST", "/api/v1/link-intents", {"local_account_ref": local_ref}, {"Idempotency-Key": tx["state"]})
            tx.update(intent=intent, nonce=intent["nonce"])
        self.transactions.put(tx)
        challenge = base64.urlsafe_b64encode(hashlib.sha256(tx["verifier"].encode()).digest()).rstrip(b"=").decode()
        return endpoint + "?" + urlencode(dict(client_id=self.config.client_id, redirect_uri=self.config.redirect_uri, response_type="code", scope=" ".join(scopes or ["openid", "profile", "email"]), state=tx["state"], nonce=tx["nonce"], code_challenge=challenge, code_challenge_method="S256"))

    def begin_registration(self, browser_binding: str, *, new_local_account_ref: str, return_to: str = "/", scopes: list[str] | None = None) -> str:
        """Use a newly reserved ID under the application's signup policy.
        Atomically reject existing IDs when creating the user and pending link.
        """
        if not new_local_account_ref:
            raise FastCASError("new_account_required", "New local account reference required")
        return self._begin("register", browser_binding, return_to, scopes, new_local_account_ref)

    def finish_login(self, callback: str, browser_binding: str, *, local_account_ref: str = "", local_session_id: str = "") -> dict:
        actual, expected = urlparse(callback), urlparse(self.config.redirect_uri)
        if (actual.scheme, actual.netloc, actual.path) != (expected.scheme, expected.netloc, expected.path):
            raise FastCASError("callback_mismatch", "Unexpected callback URL")
        query = parse_qs(actual.query)
        state = query.get("state", [""])[0]
        if not state:
            raise FastCASError("state_missing", "Callback state required")
        tx = self.transactions.take(state)
        if not tx or tx["expires_at"] <= time.time() or not hmac.compare_digest(tx["binding_hash"], _hash(browser_binding)):
            raise FastCASError("transaction_invalid", "Transaction expired, consumed, or belongs to another browser")
        if tx["purpose"] == "link" and (tx["local_account_ref"] != local_account_ref or tx["local_session_id"] != local_session_id):
            raise FastCASError("local_account_changed", "Local account changed during linking")
        if "error" in query or not query.get("code"):
            raise FastCASError("authorization_denied", "Authorization was not granted")
        tokens = self._token(dict(grant_type="authorization_code", code=query["code"][0], redirect_uri=self.config.redirect_uri, code_verifier=tx["verifier"]))
        claims = self._claims(tokens.get("id_token", ""), self.config.client_id, nonce=tx["nonce"], access_token=tokens["access_token"])
        profile = self._json(self.http.get(self.discovery()["userinfo_endpoint"], headers={"Authorization": "Bearer "+tokens["access_token"]}))
        if profile.get("sub") != claims["sub"]:
            raise FastCASError("subject_mismatch", "Userinfo subject mismatch")
        return {"identity": dict(issuer=claims["iss"], subject=claims["sub"], name=profile.get("name"), email=profile.get("email"), email_verified=profile.get("email_verified") is True, sid=claims.get("sid")), "transaction": tx, "tokens": tokens}

    def prepare_link(self, result: dict) -> dict:
        tx = result["transaction"]
        if tx["purpose"] not in {"link", "register"} or not tx.get("intent"):
            raise FastCASError("link_required", "Completed linking transaction required")
        return self._api("POST", "/api/v1/link-intents/"+quote(tx["intent"]["id"], safe="")+"/prepare", dict(nonce=tx["intent"]["nonce"], subject=result["identity"]["subject"]))

    def activate_link(self, link_id: str) -> dict:
        """Call after the local pending link is committed; persist active afterwards."""
        return self._api("POST", "/api/v1/link-intents/"+quote(link_id, safe="")+"/activate")

    def get_link(self, link_id: str) -> dict:
        return self._api("GET", "/api/v1/account-links/"+quote(link_id, safe=""))

    def resolve_link(self, subject: str) -> dict:
        return self._api("GET", "/api/v1/account-links/resolve?"+urlencode({"subject": subject}))

    def revoke_link(self, link: dict) -> dict:
        return self._api("POST", "/api/v1/account-links/"+quote(link["id"], safe="")+"/revoke", headers={"If-Match": '"'+str(link["version"])+'"'})

    def verify_access_token(self, token: str, audience: str, scopes: list[str] | None = None) -> dict:
        claims = self._claims(token, audience)
        if claims.get("token_use") != "access" or not isinstance(claims.get("scope"), str):
            raise FastCASError("wrong_token_type", "API access token required", 401)
        if not set(scopes or []).issubset(set(claims["scope"].split())):
            raise FastCASError("insufficient_scope", "Required permission missing", 403)
        return claims

    def introspect_token(self, token: str) -> dict:
        """Check central revocation after local JWT/audience/scope validation."""
        if not self.config.client_secret or not token:
            raise FastCASError("confidential_client_required", "Confidential client credential and token required")
        raw = self.discovery().get("introspection_endpoint", "")
        parsed = _url(raw, self.config.allow_loopback_http)
        if f"{parsed.scheme}://{parsed.netloc}" != self.config.issuer:
            raise FastCASError("endpoint_mismatch", "Introspection endpoint must belong to the issuer")
        result = self._json(self.http.post(raw, data={"token": token}, auth=self._auth(), follow_redirects=False))
        if type(result.get("active")) is not bool:
            raise FastCASError("invalid_response", "Invalid introspection response")
        return result

    def refresh(self, token: str) -> dict:
        return self._token(dict(grant_type="refresh_token", refresh_token=token))

    def client_credentials(self, scopes: list[str]) -> dict:
        return self._token(dict(grant_type="client_credentials", scope=" ".join(scopes)))

    def device_authorize(self, scopes: list[str]) -> dict:
        """Start a public-client device flow; the caller displays both URI and code."""
        if self.config.client_secret:
            raise FastCASError("public_client_required", "Device pairing uses a public client")
        endpoint = self.discovery().get("device_authorization_endpoint", "")
        parsed = _url(endpoint, self.config.allow_loopback_http)
        if f"{parsed.scheme}://{parsed.netloc}" != self.config.issuer:
            raise FastCASError("endpoint_mismatch", "Device endpoint must belong to the issuer")
        result = self._json(self.http.post(endpoint, data={"client_id": self.config.client_id, "scope": " ".join(scopes)}))
        verification = _url(result.get("verification_uri", ""), self.config.allow_loopback_http)
        if f"{verification.scheme}://{verification.netloc}" != self.config.issuer or not result.get("device_code") or not result.get("user_code"):
            raise FastCASError("invalid_response", "Invalid device authorization response")
        return result

    def poll_device(self, device_code: str) -> dict:
        """Poll once. authorization_pending and slow_down are raised as FastCASError."""
        if self.config.client_secret:
            raise FastCASError("public_client_required", "Device pairing uses a public client")
        tokens = self._token({"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "device_code": device_code})
        claims = self._claims(tokens.get("id_token", ""), self.config.client_id)
        access = self.verify_access_token(tokens.get("access_token", ""), self.config.client_id, ["openid"])
        if claims["sub"] != access["sub"] or access.get("service") is True:
            raise FastCASError("identity_mismatch", "Device authorization did not return a user identity", 401)
        return {"identity": {"issuer": claims["iss"], "subject": claims["sub"]}, "tokens": tokens}

    def register_device_installation(self, access_token: str, installation_id: str) -> dict:
        """Bind one approved device grant to a stable local installation."""
        result = self._json(self.http.post(self.config.issuer + "/api/v1/device-installations",
            json={"installation_id": installation_id}, headers={"Authorization": "Bearer " + access_token}))
        installation = result.get("installation")
        if not isinstance(installation, dict) or installation.get("installation_id") != installation_id or not isinstance(result.get("management_secret"), str):
            raise FastCASError("invalid_response", "Invalid installation registration response")
        return result

    def device_installation_status(self, installation_record_id: str, management_secret: str) -> dict:
        result = self._json(self.http.get(self.config.issuer + "/api/v1/device-installations/" + installation_record_id,
            headers={"Authorization": "Bearer " + management_secret}))
        if result.get("id") != installation_record_id:
            raise FastCASError("invalid_response", "Invalid installation status response")
        return result

    def revoke_device_installation(self, installation_record_id: str, management_secret: str) -> None:
        response = self.http.post(self.config.issuer + "/api/v1/device-installations/" + installation_record_id + "/revoke",
            headers={"Authorization": "Bearer " + management_secret})
        if response.status_code != 204:
            self._json(response)
            raise FastCASError("invalid_response", "Expected an empty revoke response")

    def exchange_token(self, subject_token: str, resource: str, scopes: list[str]) -> dict:
        return self._token(dict(grant_type="urn:ietf:params:oauth:grant-type:token-exchange", subject_token=subject_token, subject_token_type="urn:ietf:params:oauth:token-type:access_token", requested_token_type="urn:ietf:params:oauth:token-type:access_token", resource=resource, audience=resource, scope=" ".join(scopes)))

    def verify_logout(self, token: str) -> dict:
        claims = self._claims(token, self.config.client_id, logout=True)
        events = claims.get("events")
        if (not isinstance(events, dict) or set(events) != {"http://schemas.openid.net/event/backchannel-logout"}
            or not isinstance(events["http://schemas.openid.net/event/backchannel-logout"], dict)
            or "nonce" in claims or not claims.get("jti") or abs(time.time()-claims["iat"]) > 300):
            raise FastCASError("logout_invalid", "Invalid logout token", 401)
        return dict(id=claims["jti"], subject=claims["sub"], sid=claims.get("sid"), expires_at=claims["exp"])

    def handle_logout(self, token: str, accept_replay_id: Callable[[str, float], bool]) -> dict:
        result = self.verify_logout(token)
        if not accept_replay_id(result["id"], result["expires_at"]):
            raise FastCASError("logout_replayed", "Logout token already processed", 401)
        return dict(subject=result["subject"], sid=result["sid"])

    def diagnostics(self) -> dict:
        self.discovery()
        return dict(issuer=self.config.issuer, client_id=self.config.client_id, ready=True)

    def handle_event(self, token: str, apply: Callable[[dict], None]) -> None:
        def only_link(event):
            if event["type"] != "account_link.revoked":
                raise FastCASError("event_invalid", "Unsupported account-link event", 401)
            apply(event)
        self.handle_notification(token, only_link)

    def handle_notification(self, token: str, apply: Callable[[dict], None]) -> None:
        """Commit ID deduplication, version comparison and local updates in one
        database transaction inside apply. A committed duplicate is successful.
        A failed callback must cause the HTTP handler to return non-2xx.
        """
        if len(token) > 65536:
            raise FastCASError("event_invalid", "Event too large", 401)
        claims = self._claims(token, self.config.client_id, event=True)
        event = claims.get("event")
        link = event.get("link") if isinstance(event, dict) else None
        if (not isinstance(claims.get("jti"), str) or not claims["jti"] or abs(time.time() - claims["iat"]) > 300):
            raise FastCASError("event_invalid", "Invalid event claims", 401)
        if isinstance(event, dict) and event.get("type") == "identity.status_changed":
            if (not isinstance(event.get("subject"), str) or not event["subject"] or
                event.get("status") not in ("active", "disabled") or
                type(event.get("version")) is not int or event["version"] < 2 or "link" in event):
                raise FastCASError("event_invalid", "Invalid identity status event", 401)
            apply(dict(id=claims["jti"], type="identity.status_changed", subject=event["subject"], status=event["status"], version=event["version"]))
            return
        if (not isinstance(link, dict) or event.get("type") != "account_link.revoked" or
            link.get("client_id") != self.config.client_id or link.get("state") != "revoked" or
            any(not isinstance(link.get(key), str) or not link[key] for key in ("id", "subject", "local_account_ref")) or
            type(link.get("version")) is not int or link["version"] < 1):
            raise FastCASError("event_invalid", "Invalid account-link event", 401)
        apply(dict(id=claims["jti"], type=event["type"], link=link))

    def _auth(self):
        # OAuth Basic authentication percent-encodes both components first.
        return httpx.BasicAuth(quote(self.config.client_id, safe=""), quote(self.config.client_secret, safe=""))

    def _token(self, form: dict) -> dict:
        form = dict(form)
        if not self.config.client_secret:
            form["client_id"] = self.config.client_id
        return self._json(self.http.post(self.discovery()["token_endpoint"], data=form, auth=self._auth() if self.config.client_secret else None))

    def _api(self, method: str, path: str, body: dict | None = None, headers: dict | None = None) -> dict:
        if not self.config.client_secret:
            raise FastCASError("confidential_client_required", "Application server credential required")
        return self._json(self.http.request(method, self.config.issuer+path, json=body, headers=headers, auth=self._auth()))


class AsyncFastCAS:
    """Async facade for synchronous framework/store adapters.

    Runs blocking HTTP and database work in worker threads, never on the event
    loop. The injected store must be thread safe. HTTP timeouts bound cancelled
    calls; cancelling a coroutine cannot undo a consumed login transaction.
    """

    def __init__(self, configuration: Configuration, transactions: TransactionStore, **kwargs):
        self.sync = FastCAS(configuration, transactions, **kwargs)

    async def handle_event(self, token: str, apply: Callable[[dict], Awaitable[None]]) -> None:
        async def only_link(event):
            if event["type"] != "account_link.revoked":
                raise FastCASError("event_invalid", "Unsupported account-link event", 401)
            await apply(event)
        await self.handle_notification(token, only_link)

    async def handle_notification(self, token: str, apply: Callable[[dict], Awaitable[None]]) -> None:
        # Verify off the event loop, then await the application's async database
        # transaction on its original loop before allowing HTTP acknowledgement.
        def verified():
            events = []
            self.sync.handle_notification(token, events.append)
            return events[0]
        event = await asyncio.to_thread(verified)
        await apply(event)

    def __getattr__(self, name):
        if name.startswith("_"):
            raise AttributeError(name)
        method = getattr(self.sync, name)
        if not callable(method):
            raise AttributeError(name)
        async def call(*args, **kwargs):
            return await asyncio.to_thread(method, *args, **kwargs)
        return call
