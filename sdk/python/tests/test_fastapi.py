"""Optional FastAPI adapter transport and retry contract."""

import unittest

try:
    from fastapi import FastAPI, Request
    from fastapi.testclient import TestClient
    from fastcas import FastCASError
    from fastcas.fastapi import receive_event, receive_logout
except ImportError:
    raise unittest.SkipTest("install fastcas-sdk[fastapi] to test the optional adapter")


class FakeSDK:
    def handle_notification(self, token, apply):
        if token != "signed":
            raise FastCASError("invalid_token", "invalid", 401)
        apply({"id": "evt-1", "type": "identity.status_changed", "subject": "cas-user", "status": "disabled", "version": 2})

    def verify_logout(self, token):
        if token != "signed":
            raise FastCASError("invalid_token", "invalid", 401)
        return {"id": "logout-1", "subject": "cas-user", "sid": None}


class FastAPIAdapterTest(unittest.TestCase):
    def test_signed_transport_and_retry_boundary(self):
        seen = []
        fail = [False]
        app = FastAPI()
        sdk = FakeSDK()

        def apply(value):
            if fail[0]:
                raise RuntimeError("transaction failed")
            seen.append(value)

        @app.post("/events")
        async def events(request: Request):
            await receive_event(request, sdk, apply)
            return {"ok": True}

        @app.post("/logout")
        async def logout(request: Request):
            await receive_logout(request, sdk, apply)
            return {"ok": True}

        with TestClient(app) as client:
            self.assertEqual(client.post("/events", json={}).status_code, 415)
            self.assertEqual(client.post("/events", content="x" * 65537, headers={"content-type": "application/jwt"}).status_code, 413)
            self.assertEqual(client.post("/events", content="invalid", headers={"content-type": "application/jwt"}).status_code, 401)
            self.assertEqual(client.post("/events", content="signed", headers={"content-type": "application/jwt"}).status_code, 200)
            self.assertEqual(seen[-1]["id"], "evt-1")
            fail[0] = True
            self.assertEqual(client.post("/events", content="signed", headers={"content-type": "application/jwt"}).status_code, 503)
            fail[0] = False
            self.assertEqual(client.post("/logout", data={"other": "x"}).status_code, 400)
            self.assertEqual(client.post("/logout", data={"logout_token": "invalid"}).status_code, 401)
            self.assertEqual(client.post("/logout", data={"logout_token": "signed"}).status_code, 200)
            self.assertEqual(seen[-1]["id"], "logout-1")


if __name__ == "__main__":
    unittest.main()
