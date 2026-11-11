import asyncio
import concurrent.futures
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import Mock

import httpx

from fastcas import AsyncFastCAS, Configuration, FastCAS, FastCASError, SQLiteTransactionStore


CONTROLS = [chr(code) for code in [*range(32), 127]]
INVALID_PATHS = ["https://attacker.test", "//attacker.test", "/\\attacker.test", *[
    path for control in CONTROLS for path in (f"/{control}/attacker.test", f"/projects?query={control}", f"/projects#{control}")
]]
VALID_PATHS = ["/", "/projects?sort=recent&tag=a#details", "/项目/😀?q=中文#片段", "/encoded/%00%09%0A%0D%1F%7F%2F%5C%E4%B8%AD", "/unicode/\u0085\u2028"]
BEGIN_METHODS = [
    ("begin_login", {}),
    ("begin_link", dict(local_account_ref="local", local_session_id="session")),
    ("begin_registration", dict(new_local_account_ref="new-local")),
]


class StoreTests(unittest.TestCase):
    def test_transaction_is_consumed_once_across_connections(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/"transactions.sqlite"
            store = SQLiteTransactionStore(path)
            store.put(dict(state="one", expires_at=time.time()+300))
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
                results = list(executor.map(lambda _: SQLiteTransactionStore(path).take("one"), range(8)))
            self.assertEqual(sum(result is not None for result in results), 1)

    def test_insecure_remote_configuration_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FastCASError):
                FastCAS(Configuration("http://remote.test", "app", "http://localhost/callback", allow_loopback_http=True), SQLiteTransactionStore(Path(directory)/"transactions.sqlite"))


class ReturnPathTests(unittest.TestCase):
    def setUp(self):
        self.store = Mock()
        self.requests = []

        def respond(request):
            self.requests.append(request)
            if request.url.path == "/.well-known/openid-configuration":
                return httpx.Response(200, json=dict(
                    issuer="https://auth.example", authorization_endpoint="https://auth.example/authorize",
                    token_endpoint="https://auth.example/token", jwks_uri="https://auth.example/jwks",
                    userinfo_endpoint="https://auth.example/userinfo",
                ))
            self.assertEqual(request.url.path, "/api/v1/link-intents")
            return httpx.Response(200, json=dict(id="intent", nonce="nonce"))

        self.http = httpx.Client(transport=httpx.MockTransport(respond))
        self.addCleanup(self.http.close)
        self.config = Configuration("https://auth.example", "app", "https://app.example/callback", client_secret="test-secret")
        self.client = FastCAS(self.config, self.store, http=self.http)

    def test_rejects_ascii_controls_before_http_or_storage(self):
        for method, options in BEGIN_METHODS:
            for path in INVALID_PATHS:
                with self.subTest(method=method, path=repr(path)):
                    with self.assertRaises(FastCASError) as raised:
                        getattr(self.client, method)("b" * 32, return_to=path, **options)
                    self.assertEqual(raised.exception.code, "invalid_return_to")
        self.store.put.assert_not_called()
        self.assertEqual(self.requests, [])

    def test_preserves_valid_paths_and_default(self):
        for method, options in BEGIN_METHODS:
            for path in VALID_PATHS:
                with self.subTest(method=method, path=path):
                    authorization = getattr(self.client, method)("b" * 32, return_to=path, **options)
                    self.assertTrue(authorization.startswith("https://auth.example/authorize?"))
                    self.assertEqual(self.store.put.call_args.args[0]["return_to"], path)
            getattr(self.client, method)("b" * 32, **options)
            self.assertEqual(self.store.put.call_args.args[0]["return_to"], "/")

    def test_async_facade_rejects_ascii_controls(self):
        client = AsyncFastCAS(self.config, self.store, http=self.http)

        async def exercise():
            for method, options in BEGIN_METHODS:
                for path in INVALID_PATHS:
                    with self.subTest(method=method, path=repr(path)):
                        with self.assertRaises(FastCASError) as raised:
                            await getattr(client, method)("b" * 32, return_to=path, **options)
                        self.assertEqual(raised.exception.code, "invalid_return_to")

        asyncio.run(exercise())
        self.store.put.assert_not_called()
        self.assertEqual(self.requests, [])


if __name__ == "__main__":
    unittest.main()
