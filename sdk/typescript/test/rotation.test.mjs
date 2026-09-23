import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPair, exportJWK, SignJWT } from 'jose';
import { FastCAS } from '../dist/server.js';

test('rotation refreshes unknown kid after cooldown and removal requires a fresh verifier', async () => {
  const issuer = 'https://cas.example.test';
  const pairs = await Promise.all([generateKeyPair('RS256'), generateKeyPair('RS256')]);
  const keys = await Promise.all(pairs.map(async (pair, i) => ({ ...await exportJWK(pair.publicKey), kid: `key-${i}`, alg: 'RS256', use: 'sig' })));
  let published = [keys[0]], requests = 0, unavailable = false;
  const configuration = { issuer, clientId: 'write', redirectUri: 'https://write.example.test/callback', fetch: async url => {
    if (String(url).endsWith('/jwks')) { requests++; if (unavailable) return new Response('', { status: 503 }); return Response.json({ keys: published }); }
    return Response.json({ issuer, authorization_endpoint: issuer+'/authorize', token_endpoint: issuer+'/token', userinfo_endpoint: issuer+'/userinfo', jwks_uri: issuer+'/jwks', response_types_supported:['code'], subject_types_supported:['public'], id_token_signing_alg_values_supported:['RS256'] });
  }};
  const create = () => new FastCAS(configuration, { put: async()=>{}, take: async()=>undefined });
  const link = { id:'link', client_id:'write', local_account_ref:'local', subject:'subject', state:'revoked', version:2 };
  const sign = i => new SignJWT({event:{type:'account_link.revoked',link}}).setProtectedHeader({alg:'RS256',kid:`key-${i}`,typ:'fastcas-event+jwt'}).setIssuer(issuer).setAudience('write').setJti('event').setIssuedAt().setExpirationTime('5m').sign(pairs[i].privateKey);
  const oldToken = await sign(0), newToken = await sign(1);
  const sdk = create(); const apply = async()=>{};
  await sdk.handleEvent(oldToken, apply);
  published = keys;
  // Cooldown bounds unknown-kid network amplification; rotation can briefly retry.
  await assert.rejects(sdk.handleEvent(newToken, apply));
  assert.equal(requests, 1);
  const now = Date.now;
  Date.now = () => now()+31_000;
  try {
    await sdk.handleEvent(newToken, apply);
    await sdk.handleEvent(oldToken, apply);
    assert.equal(requests, 2);
    published = [keys[1]];
    await sdk.handleEvent(oldToken, apply); // existing cache still trusts the removed key
    sdk.resetVerificationCache();
    await assert.rejects(sdk.handleEvent(oldToken, apply));
    await sdk.handleEvent(newToken, apply);
    await assert.rejects(create().handleEvent(oldToken, apply));
    await create().handleEvent(newToken, apply);
    unavailable = true;
    await assert.rejects(create().handleEvent(newToken, apply));
  } finally { Date.now = now; }
});
