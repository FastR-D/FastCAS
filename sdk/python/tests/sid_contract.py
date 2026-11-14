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
        form = None
        for _ in range(10):
            response = browser.get(url) if form is None else browser.post(url, data=form, headers={"Origin": issuer})
            if response.status_code in (302, 303):
                url = urljoin(issuer, response.headers["location"])
                if urlparse(url).netloc != urlparse(issuer).netloc:
                    return url
                form = None
                continue
            assert response.status_code == 200
            body = response.text
            csrf = re.search(r'name="csrf" value="([^"]+)"', body)
            assert csrf, "Authorization form missing CSRF"
            form = dict(csrf=csrf[1], auth_request_id=parse_qs(urlparse(url).query)["auth_request_id"][0])
            if 'name="password"' in body:
                form.update(email="alice@example.test", password="correct horse battery staple", action="login")
            else:
                assert 'value="approve"' in body
                form["action"] = "approve"
            url = issuer + "/login"
        raise AssertionError("Authorization redirect loop")

    binding = secrets.token_urlsafe(32)
    callback = complete(sdk.begin_login(binding, scopes=["openid", "profile", "email", "offline_access"]))
    login = sdk.finish_login(callback, binding)
    assert login["identity"]["sid"], "Python SDK did not retain ID token sid"

print("FastCAS Python sid contract passed")
