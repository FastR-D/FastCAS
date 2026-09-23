import asyncio
import tempfile
import time
import unittest
from pathlib import Path

import httpx
from authlib.jose import JsonWebKey, JsonWebToken
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization
from fastcas import AsyncFastCAS, Configuration, FastCAS, FastCASError, SQLiteTransactionStore


class EventTests(unittest.TestCase):
    def test_signed_event_and_transaction_failure(self):
        private = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        key = JsonWebKey.import_key(private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()), {"kid": "events"})
        issuer = "https://cas.example.test"
        def respond(request):
            if request.url.path == "/jwks":
                return httpx.Response(200, json={"keys": [key.as_dict(is_private=False)]})
            return httpx.Response(200, json=dict(issuer=issuer, authorization_endpoint=issuer+"/authorize", token_endpoint=issuer+"/token", userinfo_endpoint=issuer+"/userinfo", jwks_uri=issuer+"/jwks"))
        with tempfile.TemporaryDirectory() as directory, httpx.Client(transport=httpx.MockTransport(respond)) as http:
            sdk = FastCAS(Configuration(issuer, "write", "https://write.example.test/callback"), SQLiteTransactionStore(Path(directory)/"tx.sqlite"), http=http)
            link = dict(id="link", client_id="write", local_account_ref="original-user", subject="cas-user", state="revoked", version=3)
            def sign(aud="write", typ="fastcas-event+jwt", body=None):
                return JsonWebToken(["RS256"]).encode(dict(alg="RS256", kid="events", typ=typ), dict(iss=issuer, aud=aud, iat=int(time.time()), exp=int(time.time())+300, jti="event-1", event=dict(type="account_link.revoked", link=body or link)), key).decode()
            applied = []
            sdk.handle_event(sign(), applied.append)
            self.assertEqual(applied[0]["id"], "event-1")
            self.assertEqual(applied[0]["link"]["local_account_ref"], "original-user")
            for token in (sign(aud="task"), sign(typ="JWT"), sign(body={**link, "client_id": "task"}), sign(body={**link, "version": True})):
                with self.assertRaises(FastCASError):
                    sdk.handle_event(token, applied.append)
            self.assertEqual(len(applied), 1)
            identity = dict(type="identity.status_changed", subject="cas-user", status="disabled", version=2)
            def sign_identity(body):
                return JsonWebToken(["RS256"]).encode(dict(alg="RS256", kid="events", typ="fastcas-event+jwt"), dict(iss=issuer, aud="write", iat=int(time.time()), exp=int(time.time())+300, jti="status-1", event=body), key).decode()
            sdk.handle_notification(sign_identity(identity), applied.append)
            self.assertEqual(applied[-1], dict(id="status-1", **identity))
            with self.assertRaises(FastCASError):
                sdk.handle_event(sign_identity(identity), applied.append)
            for invalid in (dict(identity, version=True), dict(identity, version=1), dict(identity, status="blocked"), dict(identity, subject="")):
                with self.assertRaises(FastCASError):
                    sdk.handle_notification(sign_identity(invalid), applied.append)
            def fail(event):
                raise RuntimeError("database unavailable")
            with self.assertRaisesRegex(RuntimeError, "database unavailable"):
                sdk.handle_event(sign(), fail)
            async_sdk = AsyncFastCAS(sdk.config, sdk.transactions, http=http)
            async def exercise():
                committed = []
                async def apply_async(event):
                    await asyncio.sleep(0)
                    committed.append(event["id"])
                await async_sdk.handle_event(sign(), apply_async)
                self.assertEqual(committed, ["event-1"])
                await async_sdk.handle_notification(sign_identity(identity), apply_async)
                self.assertEqual(committed, ["event-1", "status-1"])
                async def fail_async(event):
                    await asyncio.sleep(0)
                    raise RuntimeError("async transaction failed")
                with self.assertRaisesRegex(RuntimeError, "async transaction failed"):
                    await async_sdk.handle_event(sign(), fail_async)
            asyncio.run(exercise())


if __name__ == "__main__":
    unittest.main()
