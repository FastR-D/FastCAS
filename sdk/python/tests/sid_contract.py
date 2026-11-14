import os
import re
import secrets
from urllib.parse import urljoin, urlparse, parse_qs

import httpx

from fastcas import Configuration, FastCAS
from fastcas.store import TransactionStore


class MemoryTransactions(TransactionStore):
    def __init__(self):
        self.values = {}

    def put(self, transaction):
        self.values[transaction["state"]] = transaction

    def take(self, state):
        return self.values.pop(state, None)


issuer = os.environ["FASTCAS_CONTRACT_ISSUER"]

with httpx.Client(follow_redirects=False) as browser:
    sdk = FastCAS(Configuration(issuer=issuer, client_id="write", client_secret="integration-client-secret-32-characters-long", redirect_uri="http://127.0.0.1:3003/callback", allow_loopback_http=True), MemoryTransactions())

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
    callback = complete(sdk.begin_login(binding, scopes=["openid", "profile", "email", "offline_access"]))
    login = sdk.finish_login(callback, binding)
    assert login["identity"]["sid"], "Python SDK did not retain ID token sid"

print("FastCAS Python sid contract passed")
