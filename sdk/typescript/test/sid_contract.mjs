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
  let destination = new URL(url), form;
  for (let step = 0; step < 10; step++) {
    const response = await browser(destination, form);
    if (response.status === 302 || response.status === 303) {
      destination = new URL(response.headers.get("location"), issuer);
      if (destination.origin !== new URL(issuer).origin) return destination;
      form = undefined;
      continue;
    }
    assert.equal(response.status, 200);
    const body = await response.text();
    const csrf = body.match(/name="csrf" value="([^"]+)"/);
    assert.ok(csrf, "Authorization form missing CSRF");
    form = { csrf: csrf[1], auth_request_id: destination.searchParams.get("auth_request_id") };
    if (body.includes('name="password"')) Object.assign(form, { email: "alice@example.test", password: "correct horse battery staple", action: "login" });
    else { assert.ok(body.includes('value="approve"')); form.action = "approve"; }
    destination = new URL("/login", issuer);
  }
  throw new Error("Authorization redirect loop");
}

const binding = "test-browser-binding-long-enough-for-sid-contract";
const callback = await complete(await cas.beginLogin({ browserBinding: binding, scopes: ["openid", "profile", "email", "offline_access"] }));
const login = await cas.finishLogin(callback, { browserBinding: binding });
assert.ok(login.identity.sessionId, "TypeScript SDK did not retain ID token sid");
console.log("FastCAS TypeScript sid contract passed");
