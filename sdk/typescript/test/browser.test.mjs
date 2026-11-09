import test from "node:test";
import assert from "node:assert/strict";
import { loginURL, accountStatus } from "../dist/browser.js";
import { FastCAS, FastCASError } from "../dist/server.js";

const controls = [...Array.from({ length: 32 }, (_, code) => String.fromCharCode(code)), "\x7f"];
const invalidPaths = ["https://attacker.test", "//attacker.test", "/\\attacker.test", ...controls.flatMap(control => [
  `/${control}/attacker.test`, `/projects?query=${control}`, `/projects#${control}`,
])];
const validPaths = ["/", "/projects?sort=recent&tag=a#details", "/项目/😀?q=中文#片段", "/encoded/%00%09%0A%0D%1F%7F%2F%5C%E4%B8%AD", "/unicode/\u0085\u2028"];

test("browser redirects remain application relative", () => {
  assert.equal(loginURL(undefined, "/projects/one"), "/api/auth/fastcas/login?returnTo=%2Fprojects%2Fone");
  for (const path of ["https://attacker.test", "//attacker.test", "/\\attacker.test"]) assert.throws(() => loginURL(path));
});
test("configuration does not accept non-loopback insecure issuers", () => {
  const store = { async put() {}, async take() {} };
  assert.throws(() => new FastCAS({ issuer: "http://attacker.example", clientId: "app", redirectUri: "http://127.0.0.1/callback", allowLoopbackHTTP: true }, store));
  assert.throws(() => new FastCAS({ issuer: "https://auth.example/", clientId: "app", redirectUri: "https://app.example/callback" }, store));
});

test("loginURL rejects ASCII controls in the login path", () => {
  for (const path of invalidPaths) assert.throws(() => loginURL(path), /Application-relative paths required/, JSON.stringify(path));
});

test("loginURL rejects ASCII controls in returnTo", () => {
  for (const path of invalidPaths) assert.throws(() => loginURL(undefined, path), /Application-relative paths required/, JSON.stringify(path));
});

test("accountStatus rejects ASCII controls before fetching", async context => {
  const request = context.mock.method(globalThis, "fetch", async () => Response.json({ connected: true }));
  for (const path of invalidPaths) await assert.rejects(accountStatus(path), /Application-relative path required/, JSON.stringify(path));
  assert.equal(request.mock.callCount(), 0);
});

test("browser helpers preserve valid paths and defaults", async context => {
  const request = context.mock.method(globalThis, "fetch", async () => Response.json({ connected: true }));
  for (const path of validPaths) {
    assert.equal(loginURL(path), `${path}${path.includes("?") ? "&" : "?"}returnTo=%2F`);
    assert.equal(new URL(loginURL(undefined, path), "https://app.test").searchParams.get("returnTo"), path);
    assert.deepEqual(await accountStatus(path), { connected: true });
    assert.equal(request.mock.calls.at(-1).arguments[0], path);
  }
  assert.equal(loginURL(), "/api/auth/fastcas/login?returnTo=%2F");
  await accountStatus();
  assert.equal(request.mock.calls.at(-1).arguments[0], "/api/auth/fastcas/status");
});

function serverFixture() {
  const transactions = [];
  const requests = [];
  const issuer = "https://auth.example";
  const client = new FastCAS({ issuer, clientId: "app", clientSecret: "test-secret", redirectUri: "https://app.example/callback", fetch: async (url, options) => {
    requests.push({ url: String(url), method: options?.method });
    if (String(url) === `${issuer}/.well-known/openid-configuration`) return Response.json({
      issuer, authorization_endpoint: `${issuer}/authorize`, token_endpoint: `${issuer}/token`,
      userinfo_endpoint: `${issuer}/userinfo`, jwks_uri: `${issuer}/jwks`, response_types_supported: ["code"],
      subject_types_supported: ["public"], id_token_signing_alg_values_supported: ["RS256"],
    });
    assert.equal(String(url), `${issuer}/api/v1/link-intents`);
    return Response.json({ id: "intent", nonce: "nonce" });
  } }, { async put(transaction) { transactions.push(transaction); }, async take() {} });
  return { client, transactions, requests };
}

for (const method of ["beginLogin", "beginLink", "beginRegistration"]) {
  const options = { browserBinding: "b".repeat(32), localAccountRef: "local", localSessionId: "session", newLocalAccountRef: "new-local" };
  test(`${method} rejects ASCII controls without storing a transaction or creating an intent`, async () => {
    const { client, transactions, requests } = serverFixture();
    for (const returnTo of invalidPaths) await assert.rejects(client[method]({ ...options, returnTo }), error => error instanceof FastCASError && error.code === "invalid_return_to", JSON.stringify(returnTo));
    assert.equal(transactions.length, 0);
    assert.ok(requests.every(request => request.url.endsWith("/.well-known/openid-configuration")));
  });

  test(`${method} preserves valid returnTo values and defaults`, async () => {
    const { client, transactions } = serverFixture();
    for (const returnTo of [...validPaths, undefined, ""]) {
      const authorization = await client[method]({ ...options, returnTo });
      assert.equal(authorization.origin, "https://auth.example");
      assert.equal(transactions.at(-1).returnTo, returnTo || "/");
    }
  });
}
