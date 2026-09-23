"""Real provider contract, launched by the Go HTTP integration test."""
import os
import re
import secrets
import tempfile
from pathlib import Path
from urllib.parse import urljoin, urlparse, parse_qs

import httpx
from fastcas import Configuration, FastCAS, FastCASError, SQLiteTransactionStore


issuer = os.environ["FASTCAS_CONTRACT_ISSUER"]


def rejected(call, code=None):
    try:
        call()
    except FastCASError as error:
        if code:
            assert error.code == code, error.code
        return
    raise AssertionError("Expected rejection")


with tempfile.TemporaryDirectory() as temp, httpx.Client(follow_redirects=False) as browser:
    sdk = FastCAS(Configuration(issuer=issuer, client_id="write", client_secret="integration-client-secret-32-characters-long", redirect_uri="http://127.0.0.1:3003/callback", allow_loopback_http=True), SQLiteTransactionStore(Path(temp)/"transactions.sqlite"))

    def complete(url):
        response = browser.get(url)
        assert response.status_code == 302
        login_url = urljoin(issuer, response.headers["location"])
        request_id = parse_qs(urlparse(login_url).query)["auth_request_id"][0]
        body = browser.get(login_url).text
        if 'name="password"' in body:
            csrf = re.search(r'name="csrf" value="([^"]+)"', body)[1]
            response = browser.post(issuer+"/login", data=dict(csrf=csrf, auth_request_id=request_id, email="alice@example.test", password="correct horse battery staple", action="login"), headers={"Origin": issuer})
            assert response.status_code == 303
            body = browser.get(urljoin(issuer, response.headers["location"])).text
        csrf = re.search(r'name="csrf" value="([^"]+)"', body)[1]
        response = browser.post(issuer+"/login", data=dict(csrf=csrf, auth_request_id=request_id, action="approve"), headers={"Origin": issuer})
        assert response.status_code == 303
        response = browser.get(urljoin(issuer, response.headers["location"]))
        assert response.status_code == 302
        return response.headers["location"]

    binding = secrets.token_urlsafe(32)
    callback = complete(sdk.begin_login(binding, scopes=["openid", "profile", "email", "offline_access", "research:read"]))
    result = sdk.finish_login(callback, binding)
    assert result["identity"]["email"] == "alice@example.test"
    assert result["identity"]["name"] == "Alice"
    rejected(lambda: sdk.finish_login(callback, binding), "transaction_invalid")
    sdk.verify_access_token(result["tokens"]["access_token"], "write")
    assert sdk.introspect_token(result["tokens"]["access_token"])["active"] is True
    rejected(lambda: sdk.verify_access_token(result["tokens"]["id_token"], "write"), "wrong_token_type")
    kwargs = dict(local_account_ref="python-original-user", local_session_id="python-local-session")
    linked = sdk.finish_login(complete(sdk.begin_link(binding, **kwargs)), binding, **kwargs)
    prepared = sdk.prepare_link(linked)
    assert prepared["state"] == "prepared"
    rejected(lambda: sdk.resolve_link(result["identity"]["subject"]))
    active = sdk.activate_link(prepared["id"])
    assert sdk.resolve_link(result["identity"]["subject"])["local_account_ref"] == "python-original-user"
    csrf = browser.get(issuer + "/api/v1/me").json()["csrf"]
    consent = browser.post(issuer + "/api/v1/me/delegations", json=dict(caller_client="write", target_client="research", resource="research-api", scope="research:read", active=True), headers={"Origin": issuer, "X-CSRF-Token": csrf})
    assert consent.status_code == 204, consent.text
    delegated = sdk.exchange_token(result["tokens"]["access_token"], "research-api", ["research:read"])
    assert delegated["issued_token_type"] == "urn:ietf:params:oauth:token-type:access_token"
    assert "refresh_token" not in delegated
    claims = sdk.verify_access_token(delegated["access_token"], "research-api", ["research:read"])
    assert claims["sub"] == result["identity"]["subject"]
    assert claims["delegated"] is True and claims["act"]["sub"] == "write"
    rejected(lambda: sdk.exchange_token(result["tokens"]["access_token"], "research-api", ["research:write"]))
    revoked_token = browser.post(sdk.discovery()["revocation_endpoint"], data={"token": result["tokens"]["access_token"]}, auth=("write", "integration-client-secret-32-characters-long"))
    assert revoked_token.status_code == 200, revoked_token.text
    assert sdk.introspect_token(result["tokens"]["access_token"])["active"] is False
    assert sdk.revoke_link(active)["state"] == "revoked"
    rejected(lambda: sdk.activate_link(active["id"]))
    registration = sdk.finish_login(complete(sdk.begin_registration(binding, new_local_account_ref="python-new-user")), binding)
    assert registration["transaction"]["purpose"] == "register"
    new_link = sdk.prepare_link(registration)
    assert new_link["local_account_ref"] == "python-new-user"
    sdk.revoke_link(sdk.activate_link(new_link["id"]))
    rejected(lambda: sdk.begin_login(binding, return_to="//attacker.example"), "invalid_return_to")
    sdk.close()
print("FastCAS Python real-provider contract passed")
