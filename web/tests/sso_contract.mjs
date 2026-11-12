import assert from "node:assert/strict";
import { chromium } from "playwright-core";

const origin = process.env.FASTCAS_CONTRACT_ORIGIN;
const first = process.env.FASTCAS_FIRST_RP;
const second = process.env.FASTCAS_SECOND_RP;
assert(origin && first && second);
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome", headless: true, args: ["--no-sandbox"] });
try {
 for (const javaScriptEnabled of [false, true]) {
  // Server redirects must work even without JavaScript or a consent app.
  const context = await browser.newContext({ javaScriptEnabled });
  const page = await context.newPage();
  const errors = [];
  const documents = [];
  let logins = 0;
  page.on("request", request => {
    if (request.url() === origin + "/login" && request.method() === "POST" && request.postData()?.includes("action=login")) logins++;
  });
  page.on("response", response => {
    if (response.request().isNavigationRequest() && response.url().startsWith(origin) && response.status() === 200) documents.push(response.url());
  });
  page.on("console", message => { if (/Content Security Policy|form-action/.test(message.text())) errors.push(message.text()); });
  async function workspace(rp) {
    await page.waitForURL(rp + "/callback?**");
    assert.equal(await page.locator("h1").innerText(), "项目工作台");
  }
  async function login() {
    await page.locator('input[name="email"]').fill("alice@example.test");
    await page.locator('input[name="password"]').fill("correct horse battery staple");
    await page.getByRole('button', { name: '登录', exact: true }).click();
  }
  await page.goto(first + "/start");
  await login();
  await workspace(first);
  assert.equal(logins, 1);
  let before = documents.length;
  await page.goto(second + "/start");
  await workspace(second);
  assert.equal(logins, 1);
  assert.equal(documents.length, before, "existing session displayed a CAS document");
  await page.goto(first + "/start?prompt=none");
  await workspace(first);
  assert.equal(documents.length, before, "silent check displayed CAS UI");
  await page.goto(second + "/start?prompt=login");
  await login();
  await workspace(second);
  assert.equal(logins, 2);
  await page.goto(first + "/start?prompt=consent");
  await page.getByRole('button', { name: '确认并继续', exact: true }).click();
  await workspace(first);
  const anonymous = await browser.newContext();
  const silent = await anonymous.newPage();
  let casDocuments = 0;
  silent.on("response", response => { if (response.request().isNavigationRequest() && response.url().startsWith(origin) && response.status() === 200) casDocuments++; });
  await silent.goto(second + "/start?prompt=none");
  await silent.waitForURL(second + "/callback?**");
  assert.equal(await silent.locator("h1").innerText(), "login_required");
  assert.equal(casDocuments, 0);
  assert.deepEqual(errors, []);
  console.log(`SSO: first login, cross-project reuse, silent success/failure, forced login and explicit consent passed in Chrome (JavaScript=${javaScriptEnabled})`);
  await context.close();
  await anonymous.close();
 }
} finally {
  await browser.close();
}
