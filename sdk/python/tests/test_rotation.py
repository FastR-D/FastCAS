import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch
import httpx
from authlib.jose import JsonWebKey, JsonWebToken
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization
from fastcas import Configuration, FastCAS, FastCASError, SQLiteTransactionStore

class RotationTests(unittest.TestCase):
    def test_rotation_cooldown_removal_and_outage(self):
        issuer = 'https://cas.example.test'
        keys = []
        for i in range(2):
            key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
            keys.append(JsonWebKey.import_key(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()), {'kid':f'key-{i}'}))
        published = [keys[0]]
        requests = 0
        unavailable = False
        def respond(request):
            nonlocal requests
            if request.url.path == '/jwks':
                requests += 1
                return httpx.Response(503) if unavailable else httpx.Response(200,json={'keys':[key.as_dict(is_private=False) for key in published]})
            return httpx.Response(200,json=dict(issuer=issuer,authorization_endpoint=issuer+'/authorize',token_endpoint=issuer+'/token',userinfo_endpoint=issuer+'/userinfo',jwks_uri=issuer+'/jwks'))
        link = dict(id='link',client_id='write',local_account_ref='local',subject='subject',state='revoked',version=2)
        def sign(i):
            return JsonWebToken(['RS256']).encode(dict(alg='RS256',kid=f'key-{i}',typ='fastcas-event+jwt'),dict(iss=issuer,aud='write',iat=int(time.time()),exp=int(time.time())+300,jti='event',event=dict(type='account_link.revoked',link=link)),keys[i]).decode()
        old, new = sign(0), sign(1)
        with tempfile.TemporaryDirectory() as directory, httpx.Client(transport=httpx.MockTransport(respond)) as http:
            store = SQLiteTransactionStore(Path(directory)/'tx.sqlite')
            def create():
                return FastCAS(Configuration(issuer,'write','https://write.example.test/callback'),store,http=http)
            sdk = create()
            apply = lambda event: None
            sdk.handle_event(old,apply)
            published = keys
            with self.assertRaises(FastCASError): sdk.handle_event(new,apply)
            self.assertEqual(requests,1)
            future = time.monotonic()+31
            with patch('fastcas.client.time.monotonic',return_value=future):
                sdk.handle_event(new,apply)
                sdk.handle_event(old,apply)
                self.assertEqual(requests,2)
                published = [keys[1]]
                sdk.handle_event(old,apply)
                with self.assertRaises(FastCASError): create().handle_event(old,apply)
                sdk.reset_verification_cache()
                with self.assertRaises(FastCASError): sdk.handle_event(old,apply)
                sdk.handle_event(new,apply)
                create().handle_event(new,apply)
                unavailable = True
                with self.assertRaises(FastCASError): create().handle_event(new,apply)

if __name__ == '__main__': unittest.main()
