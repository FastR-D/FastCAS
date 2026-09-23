// Executed against the real Go HTTP provider and an isolated PostgreSQL schema.
// The in-memory transaction store is a test fixture, never a production default.
import assert from "node:assert/strict";
import { FastCAS } from "../dist/server.js";

const issuer = process.env.FASTCAS_CONTRACT_ISSUER;
if (!issuer) throw new Error("FASTCAS_CONTRACT_ISSUER required");
const transactions = new Map();
const store = { async put(value) { transactions.set(value.state, value); }, async take(state) { const value = transactions.get(state); transactions.delete(state); return value; } };
const cas = new FastCAS({ issuer, clientId: "write", clientSecret: "integration-client-secret-32-characters-long", redirectUri: "http://127.0.0.1:3003/callback", allowLoopbackHTTP: true }, store);
const jar = new Map();
async function browser(url, form) {
  const response = await fetch(url, { redirect: "manual", method: form ? "POST" : "GET", headers: { cookie: [...jar].map(([key, value]) => `${key}=${value}`).join("; "), ...(form ? { "content-type": "application/x-www-form-urlencoded", origin: issuer } : {}) }, body: form ? new URLSearchParams(form) : undefined });
  for (const cookie of response.headers.getSetCookie()) { const [entry] = cookie.split(";"); const at = entry.indexOf("="); jar.set(entry.slice(0, at), entry.slice(at + 1)); }
  return response;
}
async function complete(url) {
  let response = await browser(url);
  assert.equal(response.status, 302);
  let login = new URL(response.headers.get("location"), issuer);
  let body = await (await browser(login)).text();
  const request = login.searchParams.get("auth_request_id");
  if (body.includes('name="password"')) {
    const csrf = body.match(/name="csrf" value="([^"]+)"/)[1];
    response = await browser(issuer + "/login", { csrf, auth_request_id: request, email: "alice@example.test", password: "correct horse battery staple", action: "login" });
    assert.equal(response.status, 303);
    body = await (await browser(new URL(response.headers.get("location"), issuer))).text();
  }
  const csrf = body.match(/name="csrf" value="([^"]+)"/)[1];
  response = await browser(issuer + "/login", { csrf, auth_request_id: request, action: "approve" });
  assert.equal(response.status, 303);
  response = await browser(new URL(response.headers.get("location"), issuer));
  assert.equal(response.status, 302);
  return new URL(response.headers.get("location"));
}
const binding = "test-browser-binding-long-enough-for-entropy-contract";
const callback = await complete(await cas.beginLogin({ browserBinding: binding, scopes: ["openid", "profile", "email", "offline_access", "research:read"] }));
const login = await cas.finishLogin(callback, { browserBinding: binding });
assert.equal(login.identity.email, "alice@example.test");
assert.equal(login.identity.name, "Alice");
assert.ok(login.identity.subject);
await assert.rejects(cas.finishLogin(callback, { browserBinding: binding }), { code: "transaction_invalid" });
await assert.rejects(cas.resolveLink(login.identity.subject), { status: 401 });
await cas.verifyAccessToken(login.tokens.access_token, "write");
await assert.rejects(cas.verifyAccessToken(login.tokens.id_token, "write"), { code: "wrong_token_type" });
const input = { browserBinding: binding, localAccountRef: "original-local-user", localSessionId: "original-local-session" };
const linkedLogin = await cas.finishLogin(await complete(await cas.beginLink(input)), input);
const pending = await cas.prepareLink(linkedLogin);
assert.equal(pending.state, "prepared");
await assert.rejects(cas.resolveLink(login.identity.subject));
const active = await cas.activateLink(pending.id);
assert.equal(active.state, "active");
assert.equal((await cas.resolveLink(login.identity.subject)).local_account_ref, "original-local-user");
const { csrf } = await (await browser(issuer + "/api/v1/me")).json();
const consent = await fetch(issuer + "/api/v1/me/delegations", { method: "POST", redirect: "manual", headers: { "content-type": "application/json", origin: issuer, "x-csrf-token": csrf, cookie: [...jar].map(([key, value]) => `${key}=${value}`).join("; ") }, body: JSON.stringify({ caller_client: "write", target_client: "research", resource: "research-api", scope: "research:read", active: true }) });
assert.equal(consent.status, 204);
const delegated = await cas.exchangeToken(login.tokens.access_token, "research-api", ["research:read"]);
assert.equal(delegated.issued_token_type, "urn:ietf:params:oauth:token-type:access_token");
assert.equal(delegated.refresh_token, undefined);
const delegatedClaims = await cas.verifyAccessToken(delegated.access_token, "research-api", ["research:read"]);
assert.equal(delegatedClaims.sub, login.identity.subject);
assert.equal(delegatedClaims.delegated, true);
assert.equal(delegatedClaims.act?.sub, "write");
const resource = new FastCAS({ issuer, clientId: "research", clientSecret: "integration-client-secret-32-characters-long", redirectUri: "http://127.0.0.1:8787/callback", allowLoopbackHTTP: true }, store);
assert.equal((await resource.introspectToken(delegated.access_token)).active, true);
await assert.rejects(cas.exchangeToken(login.tokens.access_token, "research-api", ["research:write"]));
const revoked = await cas.revokeLink(active);
assert.equal((await resource.introspectToken(delegated.access_token)).active, false);
assert.equal(revoked.state, "revoked");
await assert.rejects(cas.resolveLink(login.identity.subject));
await assert.rejects(cas.activateLink(active.id));
const registration = await cas.finishLogin(await complete(await cas.beginRegistration({ browserBinding: binding, newLocalAccountRef: "new-local-account" })), { browserBinding: binding });
assert.equal(registration.transaction.purpose, "register");
const newLink = await cas.prepareLink(registration);
assert.equal(newLink.local_account_ref, "new-local-account");
await cas.revokeLink(await cas.activateLink(newLink.id));
await assert.rejects(cas.beginLogin({ browserBinding: binding, returnTo: "//attacker.example" }), { code: "invalid_return_to" });
console.log("FastCAS TypeScript real-provider contract passed");
