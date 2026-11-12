import time
from urllib.parse import urlparse, parse_qs

import httpx
from authlib.jose import JsonWebKey, JsonWebToken
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization

from fastcas import Configuration, FastCAS


class Transactions:
    def __init__(self):
        self.values = {}
    def put(self, tx):
        self.values[tx['state']] = tx
    def take(self, state):
        return self.values.pop(state, None)


def test_verified_userinfo_role_is_preserved():
    issuer = 'https://cas.test'
    private = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    key = JsonWebKey.import_key(private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()), {'kid': 'test'})
    nonce = ''
    role = 'admin'
    def respond(request):
        if request.url.path.endswith('openid-configuration'):
            return httpx.Response(200, json=dict(issuer=issuer, authorization_endpoint=issuer+'/authorize', token_endpoint=issuer+'/token', jwks_uri=issuer+'/jwks', userinfo_endpoint=issuer+'/userinfo'))
        if request.url.path == '/jwks':
            return httpx.Response(200, json={'keys': [key.as_dict(is_private=False)]})
        if request.url.path == '/token':
            token = JsonWebToken(['RS256']).encode({'alg': 'RS256', 'kid': 'test'}, dict(iss=issuer, sub='subject', aud='app', nonce=nonce, iat=int(time.time()), exp=int(time.time())+300, role='admin'), key).decode()
            return httpx.Response(200, json={'access_token': 'access', 'token_type': 'Bearer', 'id_token': token})
        if request.url.path == '/userinfo':
            return httpx.Response(200, json={'sub': 'subject', 'name': 'User', 'role': role})
        raise AssertionError(request.url)
    with httpx.Client(transport=httpx.MockTransport(respond)) as http:
        sdk = FastCAS(Configuration(issuer, 'app', 'https://app.test/callback'), Transactions(), http=http)
        for role in ('admin', 'member'):
            query = parse_qs(urlparse(sdk.begin_login('browser-binding-with-at-least-32-characters')).query)
            nonce = query['nonce'][0]
            result = sdk.finish_login('https://app.test/callback?state='+query['state'][0]+'&code=code', 'browser-binding-with-at-least-32-characters')
            assert result['identity']['role'] == role
