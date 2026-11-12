import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPair, exportJWK, SignJWT } from 'jose';
import { FastCAS } from '../dist/server.js';

test('verified userinfo supplies the current platform role', async () => {
  const issuer='https://cas.test', pair=await generateKeyPair('RS256');
  const key={...await exportJWK(pair.publicKey),kid:'test',alg:'RS256',use:'sig'};
  const transactions=new Map(); let nonce='',role='admin';
  const sdk=new FastCAS({issuer,clientId:'app',redirectUri:'https://app.test/callback',fetch:async url=>{
    const path=new URL(url).pathname;
    if(path.endsWith('openid-configuration')) return Response.json({issuer,authorization_endpoint:issuer+'/authorize',token_endpoint:issuer+'/token',userinfo_endpoint:issuer+'/userinfo',jwks_uri:issuer+'/jwks',response_types_supported:['code'],subject_types_supported:['public'],id_token_signing_alg_values_supported:['RS256']});
    if(path==='/jwks')return Response.json({keys:[key]});
    if(path==='/userinfo')return Response.json({sub:'subject',name:'User',role});
    if(path==='/token')return Response.json({access_token:'access',token_type:'Bearer',id_token:await new SignJWT({nonce,role:'admin'}).setProtectedHeader({alg:'RS256',kid:'test'}).setIssuer(issuer).setSubject('subject').setAudience('app').setIssuedAt().setExpirationTime('5m').sign(pair.privateKey)});
    throw new Error('Unexpected endpoint');
  }},{put:async tx=>{transactions.set(tx.state,tx)},take:async state=>{const tx=transactions.get(state);transactions.delete(state);return tx}});
  for(role of ['admin','member']){
    const url=await sdk.beginLogin({browserBinding:'browser-binding-with-at-least-32-characters'});nonce=url.searchParams.get('nonce');
    const result=await sdk.finishLogin(new URL('https://app.test/callback?state='+url.searchParams.get('state')+'&code=code'),{browserBinding:'browser-binding-with-at-least-32-characters'});
    assert.equal(result.identity.role,role);
  }
});
