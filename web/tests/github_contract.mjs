import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright-core";

const origin = process.env.FASTCAS_CONTRACT_ORIGIN;
const scenario = process.env.FASTCAS_GITHUB_SCENARIO;
assert(origin && scenario);
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome",
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => {
    if (message.type() === "error" && /Content Security Policy|form-action/.test(message.text())) errors.push(message.text());
  });
  // Simulate GitHub's response without real OAuth credentials or network calls.
  // Playwright only routes the first request in an HTTP redirect chain, so the
  // native signup POST preserves Set-Cookie and substitutes the final callback.
  function callbackURL(authorization) {
    assert.equal(authorization.searchParams.get("code_challenge_method"), "S256");
    assert(authorization.searchParams.get("code_challenge"));
    if (scenario === "register") assert.equal(authorization.searchParams.get("scope"), "user:email read:org");
    return `${origin}/auth/github/callback?code=test-code&state=${encodeURIComponent(authorization.searchParams.get("state"))}`;
  }
  await page.route(origin + "/auth/github/start", async route => {
    const response = await route.fetch({ maxRedirects: 0 });
    assert.equal(response.status(), 303);
    const authorization = new URL(response.headers().location);
    assert.equal(authorization.origin, "https://github.com");
    await route.fulfill({ response, headers: { ...response.headers(), location: callbackURL(authorization) } });
  });
  await page.route("https://github.com/**", async route => {
    const authorization = new URL(route.request().url());
    assert.equal(authorization.pathname, "/login/oauth/authorize");
    await route.fulfill({ status: 302, headers: { location: callbackURL(authorization) } });
  });
  await page.goto(origin + "/login");
  await page.getByRole("button", { name: "使用 GitHub 注册", exact: true }).waitFor();
  assert.match(await page.locator("main").innerText(), /FastR-D 组织正式成员/);
  if (scenario === "register") {
    await page.getByRole("tab", { name: "注册", exact: true }).click();
    await page.getByRole("button", { name: "使用 GitHub 注册", exact: true }).click();
  } else {
    await page.getByLabel("邮箱", { exact: true }).fill("existing@example.test");
    await page.getByLabel("密码", { exact: true }).fill("existing-password-123");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.getByRole("button", { name: "绑定 GitHub", exact: true }).click();
  }
  await page.waitForURL(origin + "/console/**");
  await page.getByText("已绑定 GitHub：octocat", { exact: true }).waitFor();
  await mkdir("../.local/ui-verification", { recursive: true });
  await page.screenshot({ path: `../.local/ui-verification/github-${scenario}.png`, fullPage: true });
  if (scenario === "register") {
    await page.getByLabel("新 FastCAS 密码", { exact: true }).fill("first-local-password-123");
    await page.getByLabel("确认 FastCAS 新密码", { exact: true }).fill("first-local-password-123");
    await page.getByRole("button", { name: "设置 FastCAS 密码", exact: true }).click();
    await page.waitForURL(/\/login\?notice=password-changed$/);
    await page.getByLabel("邮箱", { exact: true }).fill("octocat@example.test");
    await page.getByLabel("密码", { exact: true }).fill("first-local-password-123");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.getByText("已绑定 GitHub：octocat", { exact: true }).waitFor();
  }
  assert.deepEqual(errors, []);
  console.log(`GitHub ${scenario}: browser navigation, binding display and account actions passed`);
} finally {
  await browser.close();
}
