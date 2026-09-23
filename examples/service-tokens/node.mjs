/** Run a least-privilege service-token check against a configured FastCAS issuer. */
import {FastCAS} from '../../sdk/typescript/dist/server.js';

const {FASTCAS_ISSUER:issuer, FASTCAS_CLIENT_ID:clientId, FASTCAS_CLIENT_SECRET:clientSecret,
  FASTCAS_AUDIENCE:audience, FASTCAS_SCOPE:scope}=process.env;
if(!issuer||!clientId||!clientSecret||!audience||!scope)throw Error('Missing FASTCAS service example configuration');
const transactions={
  async put(){throw Error('This service example cannot start browser login')},
  async take(){throw Error('This service example cannot consume browser callbacks')},
};
const sdk=new FastCAS({issuer,clientId,clientSecret,redirectUri:issuer+'/unused-service-callback',
  allowLoopbackHTTP:process.env.FASTCAS_ALLOW_LOOPBACK_HTTP==='true'},transactions);
const token=(await sdk.clientCredentials([scope])).access_token;
if(typeof token!=='string')throw Error('Missing service access token');
const claims=await sdk.verifyAccessToken(token,audience,[scope]);
const current=await sdk.introspectToken(token);
if(claims.sub!==clientId||claims.service!==true||current.active!==true)throw Error('Service token identity or central status mismatch');
console.log(JSON.stringify({client_id:clientId,audience,scope,active:true}));
