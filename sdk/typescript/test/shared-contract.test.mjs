import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {FastCAS} from '../dist/server.js';
const fixture=JSON.parse(readFileSync(new URL('../../../contracts/access-tokens.json',import.meta.url)));
for(const vector of fixture.cases)test(`shared access token: ${vector.name}`,async()=>{
 const issuer=fixture.issuer;
 const sdk=new FastCAS({issuer,clientId:'fixture-client',redirectUri:'https://rp.example.test/callback',fetch:async url=>{
  if(String(url)===issuer+'/jwks')return Response.json(fixture.jwks);
  if(String(url)===issuer+'/.well-known/openid-configuration')return Response.json({issuer,authorization_endpoint:issuer+'/authorize',token_endpoint:issuer+'/token',jwks_uri:issuer+'/jwks',userinfo_endpoint:issuer+'/userinfo',response_types_supported:['code'],subject_types_supported:['public'],id_token_signing_alg_values_supported:['RS256']});
  throw Error('Unexpected network request');
 }},{put:async()=>{},take:async()=>undefined});
 const check=()=>sdk.verifyAccessToken(vector.token,vector.audience,vector.scopes);
 if(vector.valid)assert.equal((await check()).iss,issuer);else await assert.rejects(check);
});
