"""The same signed positive/negative vectors used by Go and TypeScript."""
import json
import tempfile
import unittest
from pathlib import Path
import httpx
from fastcas import Configuration, FastCAS, FastCASError, SQLiteTransactionStore

FIXTURE = json.loads((Path(__file__).resolve().parents[3]/'contracts/access-tokens.json').read_text())

class SharedContractTests(unittest.TestCase):
    def test_access_token_vectors(self):
        issuer = FIXTURE['issuer']
        def respond(request):
            if str(request.url) == issuer + '/jwks':
                return httpx.Response(200, json=FIXTURE['jwks'])
            if str(request.url) == issuer + '/.well-known/openid-configuration':
                return httpx.Response(200, json=dict(issuer=issuer, authorization_endpoint=issuer+'/authorize', token_endpoint=issuer+'/token', userinfo_endpoint=issuer+'/userinfo', jwks_uri=issuer+'/jwks'))
            raise AssertionError('Unexpected network request')
        with tempfile.TemporaryDirectory() as directory, httpx.Client(transport=httpx.MockTransport(respond)) as http:
            for vector in FIXTURE['cases']:
                with self.subTest(vector=vector['name']):
                    sdk = FastCAS(Configuration(issuer,'fixture-client','https://rp.example.test/callback'),SQLiteTransactionStore(Path(directory)/'transactions.sqlite'),http=http)
                    if vector['valid']:
                        self.assertEqual(sdk.verify_access_token(vector['token'],vector['audience'],vector['scopes'])['iss'],issuer)
                    else:
                        with self.assertRaises(FastCASError):
                            sdk.verify_access_token(vector['token'],vector['audience'],vector['scopes'])
