import test from "node:test";
import assert from "node:assert/strict";
import { loginURL } from "../dist/browser.js";
import { FastCAS } from "../dist/server.js";

test("browser redirects remain application relative", () => {
  assert.equal(loginURL(undefined, "/projects/one"), "/api/auth/fastcas/login?returnTo=%2Fprojects%2Fone");
  for (const path of ["https://attacker.test", "//attacker.test", "/\\attacker.test"]) assert.throws(() => loginURL(path));
});
test("configuration does not accept non-loopback insecure issuers", () => {
  const store = { async put() {}, async take() {} };
  assert.throws(() => new FastCAS({ issuer: "http://attacker.example", clientId: "app", redirectUri: "http://127.0.0.1/callback", allowLoopbackHTTP: true }, store));
  assert.throws(() => new FastCAS({ issuer: "https://auth.example/", clientId: "app", redirectUri: "https://app.example/callback" }, store));
});
