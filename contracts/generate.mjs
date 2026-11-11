// Synthetic offline test vectors. The ephemeral private keys are never saved.
import {generateKeyPairSync, createSign, createHmac} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const {privateKey,publicKey}=generateKeyPairSync('rsa',{modulusLength:2048});
const other=generateKeyPairSync('rsa',{modulusLength:2048});
const issuer='https://cas.example.test';
const jwk={...publicKey.export({format:'jwk'}),kid:'contract-fixture',alg:'RS256',use:'sig'};
const encode=value=>Buffer.from(JSON.stringify(value)).toString('base64url');
function sign(claims,header={},key=privateKey){
 const input=encode({alg:'RS256',kid:jwk.kid,typ:'JWT',...header})+'.'+encode(claims);
 return input+'.'+createSign('RSA-SHA256').update(input).sign(key).toString('base64url');
}
const base={iss:issuer,sub:'fixture-user',aud:['resource-api'],iat:1700000000,exp:4102444800,client_id:'fixture-client',scope:'profile:read profile:write',token_use:'access'};
const cases=[];
function add(name,patch={},valid=false,{remove=[],header={},key=privateKey,scopes=['profile:read']}={}){
 const claims={...base,...patch};for(const field of remove)delete claims[field];
 cases.push({name,token:sign(claims,header,key),audience:'resource-api',scopes,valid});
}
add('user-access',{},true);
add('service-access',{sub:'fixture-client',service:true},true);
add('delegated-access',{act:{sub:'fixture-caller'},delegated:true},true);
add('wrong-issuer',{iss:'https://unexpected.example.test'});
add('wrong-audience',{aud:['other-resource']});
add('expired',{exp:1700000300});
add('future-not-before',{nbf:4102444700});
add('future-issued-at',{iat:4102444700});
add('id-token-as-access',{token_use:'id'});
add('missing-expiry',{},false,{remove:['exp']});
add('missing-issued-at',{},false,{remove:['iat']});
add('missing-subject',{},false,{remove:['sub']});
add('empty-subject',{sub:''});
add('missing-scope',{},false,{remove:['scope'],scopes:[]});
add('array-scope',{scope:['profile:read']});
add('insufficient-scope',{scope:'profile:write'});
add('invalid-signature',{},false,{key:other.privateKey});
add('unknown-key',{},false,{header:{kid:'unregistered-key'}});
const unsigned=encode({alg:'HS256',kid:jwk.kid,typ:'JWT'})+'.'+encode(base);
cases.push({name:'symmetric-algorithm',token:unsigned+'.'+createHmac('sha256','synthetic-fixture-secret').update(unsigned).digest('base64url'),audience:'resource-api',scopes:['profile:read'],valid:false});
writeFileSync(new URL('access-tokens.json',import.meta.url),JSON.stringify({version:1,issuer,jwks:{keys:[jwk]},cases},null,2)+'\n');
