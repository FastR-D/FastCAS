import test from "node:test";
import assert from "node:assert/strict";
import { generateKeyPair, exportJWK, SignJWT } from "jose";
import { FastCAS } from "../dist/server.js";

test("signed events validate audience and type before application transaction", async () => {
  const { privateKey, publicKey } = await generateKeyPair("RS256");
  const key = { ...await exportJWK(publicKey), kid: "events", alg: "RS256", use: "sig" };
  const issuer = "https://cas.example.test";
  const sdk = new FastCAS({ issuer, clientId: "write", redirectUri: "https://write.example.test/callback", fetch: async url => {
    if (String(url).endsWith("/jwks")) return Response.json({ keys: [key] });
    if (String(url).includes("/.well-known/")) return Response.json({ issuer, authorization_endpoint: issuer + "/authorize", token_endpoint: issuer + "/token", userinfo_endpoint: issuer + "/userinfo", jwks_uri: issuer + "/jwks", response_types_supported: ["code"], subject_types_supported: ["public"], id_token_signing_alg_values_supported: ["RS256"] });
    throw Error("Unexpected endpoint");
  } }, { put: async () => {}, take: async () => undefined });
  const link = { id: "link", client_id: "write", local_account_ref: "local-user", subject: "cas-user", state: "revoked", version: 3 };
  const sign = (audience, typ, event = { type: "account_link.revoked", link }) => new SignJWT({ event }).setProtectedHeader({ alg: "RS256", kid: "events", typ }).setIssuer(issuer).setAudience(audience).setJti("event-1").setIssuedAt().setExpirationTime("5m").sign(privateKey);
  let applied = 0;
  const apply = async event => { assert.equal(event.id, "event-1"); assert.equal(event.link.version, 3); applied++; };
  await sdk.handleEvent(await sign("write", "fastcas-event+jwt"), apply);
  await assert.rejects(sdk.handleEvent(await sign("task", "fastcas-event+jwt"), apply));
  await assert.rejects(sdk.handleEvent(await sign("write", "JWT"), apply));
  await assert.rejects(sdk.handleEvent(await sign("write", "fastcas-event+jwt", { type: "account_link.revoked", link: { ...link, client_id: "task" } }), apply));
  assert.equal(applied, 1);
  await assert.rejects(sdk.handleEvent(await sign("write", "fastcas-event+jwt"), async () => { throw Error("database unavailable"); }), /database unavailable/);
  const identity = { type: "identity.status_changed", subject: "cas-user", status: "disabled", version: 2 };
  const notices = [];
  await sdk.handleNotification(await sign("write", "fastcas-event+jwt", identity), async event => notices.push(event));
  assert.deepEqual(notices[0], { id: "event-1", ...identity });
  await assert.rejects(sdk.handleEvent(await sign("write", "fastcas-event+jwt", identity), apply));
  for (const invalid of [{ ...identity, status: "blocked" }, { ...identity, version: 1 }, { ...identity, version: true }, { ...identity, subject: "" }])
    await assert.rejects(sdk.handleNotification(await sign("write", "fastcas-event+jwt", invalid), async () => {}));
});
