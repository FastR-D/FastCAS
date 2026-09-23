"""Run a least-privilege service-token check against a configured FastCAS issuer."""
import json
import os

from fastcas import Configuration, FastCAS


class NoBrowserTransactions:
    def put(self, _transaction):
        raise RuntimeError("This service example cannot start browser login")

    def take(self, _state):
        raise RuntimeError("This service example cannot consume browser callbacks")


def main():
    issuer = os.environ["FASTCAS_ISSUER"].rstrip("/")
    client_id = os.environ["FASTCAS_CLIENT_ID"]
    audience = os.environ["FASTCAS_AUDIENCE"]
    scope = os.environ["FASTCAS_SCOPE"]
    sdk = FastCAS(Configuration(issuer, client_id, issuer + "/unused-service-callback",
        client_secret=os.environ["FASTCAS_CLIENT_SECRET"],
        allow_loopback_http=os.environ.get("FASTCAS_ALLOW_LOOPBACK_HTTP") == "true"), NoBrowserTransactions())
    try:
        token = sdk.client_credentials([scope])["access_token"]
        claims = sdk.verify_access_token(token, audience, [scope])
        current = sdk.introspect_token(token)
        if claims.get("sub") != client_id or claims.get("service") is not True or current.get("active") is not True:
            raise RuntimeError("Service token identity or central status mismatch")
        print(json.dumps({"client_id": client_id, "audience": audience, "scope": scope, "active": True}))
    finally:
        sdk.close()


if __name__ == "__main__":
    main()
