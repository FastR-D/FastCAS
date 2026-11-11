import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { createHmac } from "node:crypto";
import { chromium } from "playwright-core";

const {
  FASTCAS_CONTRACT_ORIGIN: origin,
  FASTCAS_CONTRACT_INVITE: invite,
  FASTCAS_CONTRACT_RECOVERY: recovery,
} = process.env;
if (!origin || !invite || !recovery)
  throw Error("browser contract configuration missing");
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome",
  headless: true,
  args: ["--no-sandbox"],
});
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
});
const page = await context.newPage();
const uiErrors = [];
const integrationChecks = [];
page.on("pageerror", (error) => uiErrors.push(error.message));
await mkdir("../.local/ui-verification", { recursive: true });
const screenshot = async (name) => {
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `../.local/ui-verification/${name}.png`,
    fullPage: true,
  });
};
const auditAction = async (name) => {
  await Promise.all([
    page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/api/v1/admin/audit-events",
    ),
    page.getByRole("button", { name }).click(),
  ]);
  await page
    .getByText("正在处理，请稍候…", { exact: true })
    .waitFor({ state: "hidden" });
};
const acceptConfirmation = async () =>
  page
    .getByRole("dialog", { name: "确认操作" })
    .getByRole("button", { name: "确认并继续" })
    .click();
// Delay only the application module: inspect the actual server-delivered page
// after the blocking bootstrap, before either React or auth controls execute.
async function firstPaint(path, stored, system, expected, denyStorage = false) {
  const isolated = await browser.newContext({ colorScheme: system });
  try {
    await isolated.addInitScript(({ stored, denyStorage }) => {
      if (stored !== null) localStorage.setItem("fastcas.color-mode", stored);
      if (denyStorage) {
        Storage.prototype.getItem = Storage.prototype.setItem = () => {
          throw new DOMException("Storage denied", "SecurityError");
        };
      }
    }, { stored, denyStorage });
    const target = await isolated.newPage();
    const errors = [];
    target.on("pageerror", (error) => errors.push(error.message));
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    await target.route(/\/assets\/(console|auth)-.*\.js$/, async (route) => {
      await held;
      await route.continue();
    });
    try {
      await target.goto(origin + path, { waitUntil: "commit" });
      await target.waitForFunction(() => window.fastCASColorMode, { timeout: 5000 });
      const controller = await target.evaluateHandle(() => window.fastCASColorMode);
      assert.equal(await target.locator("html").getAttribute("data-fastui-theme"), expected);
      release();
      await target.getByLabel("界面主题").waitFor();
      assert.equal(await target.evaluate((initial) => window.fastCASColorMode === initial, controller), true,
        "application replaced the first-paint controller");
      if (path === "/console/") {
        await target.getByLabel("一次性凭证").fill("unsubmitted-draft-token");
        await target.getByLabel("姓名").fill("主题草稿");
        await target.getByLabel("新密码", { exact: true }).fill("draft-password-123");
        await target.getByLabel("确认新密码").fill("different-password-456");
        await target.getByRole("button", { name: "创建 FastCAS 账号" }).click();
        await target.getByRole("alert").getByText("两次输入的密码不一致").waitFor();
      } else {
        await target.locator("input[name=email]").fill("draft@example.test");
      }
      await target.getByLabel("界面主题").selectOption(expected === "dark" ? "light" : "dark");
      assert.equal(await target.locator("html").getAttribute("data-fastui-theme"), expected === "dark" ? "light" : "dark");
      assert.deepEqual(errors, []);
      integrationChecks.push({ path, stored, system, denyStorage, firstPaint: expected, sameController: true });
    } finally {
      release();
    }
  } finally {
    await isolated.close();
  }
}

async function preserveSecurityDraft() {
  await page.getByLabel("界面主题").selectOption("system");
  const input = page.getByLabel("新 FastCAS 密码", { exact: true });
  await input.fill("preserved-password-draft-123");
  await input.focus();
  const original = await input.elementHandle();
  const assertDraft = async () => {
    assert.equal(await input.inputValue(), "preserved-password-draft-123");
    assert.equal(await input.evaluate((element, initial) => element === initial && element === document.activeElement, original), true,
      "theme change remounted or blurred the security form");
  };
  await page.emulateMedia({ colorScheme: "dark" });
  await page.waitForFunction(() => document.documentElement.dataset.fastuiTheme === "dark");
  await assertDraft();
  const other = await context.newPage();
  try {
    await other.goto(origin + "/console/");
    await other.getByLabel("界面主题").selectOption("light");
    await page.waitForFunction(() => document.documentElement.dataset.fastuiTheme === "light");
    await assertDraft();
  } finally {
    await other.close();
  }
  await input.fill("");
  integrationChecks.push({ securityDraft: "system-and-cross-tab", sameInput: true, focus: true });
}
page.setDefaultTimeout(5000);
function totp(uri) {
  const secret = new URL(uri).searchParams.get("secret");
  if (!secret) throw Error("TOTP secret missing");
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0,
    value = 0;
  const bytes = [];
  for (const letter of secret.replace(/=+$/, "").toUpperCase()) {
    const digit = alphabet.indexOf(letter);
    if (digit < 0) throw Error("invalid TOTP secret");
    value = (value << 5) | digit;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((value >> bits) & 255);
    }
  }
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const digest = createHmac("sha1", Buffer.from(bytes))
    .update(counter)
    .digest();
  const offset = digest[19] & 15;
  const number =
    ((digest[offset] & 127) << 24) |
    ((digest[offset + 1] & 255) << 16) |
    ((digest[offset + 2] & 255) << 8) |
    (digest[offset + 3] & 255);
  return String(number % 1000000).padStart(6, "0");
}
try {
  for (const path of ["/console/", "/login"]) {
    await firstPaint(path, "dark", "light", "dark");
    await firstPaint(path, "light", "dark", "light");
    await firstPaint(path, "invalid", "dark", "dark");
    await firstPaint(path, null, "light", "light");
    await firstPaint(path, null, "dark", "dark", true);
  }
  await page.goto(
    `${origin}/console/?flow=invite&token=${encodeURIComponent(invite)}`,
    { waitUntil: "networkidle" },
  );
  await page.waitForURL((url) => !url.search);
  assert.equal(new URL(page.url()).search, "", "invite token remained in URL");
  await page.getByLabel("姓名").fill("Invited Reader");
  await page
    .getByLabel("新密码", { exact: true })
    .fill("invited-reader-password-123");
  await page.getByLabel("确认新密码").fill("invited-reader-password-123");
  await page.getByRole("button", { name: "创建 FastCAS 账号" }).click();
  await page.getByRole("status").getByText("账号已创建").waitFor();
  await page.getByRole("link", { name: "前往登录" }).first().click();
  await page.waitForLoadState("networkidle");
  assert.equal(
    await page
      .locator("input[name=email]")
      .evaluate((element) => getComputedStyle(element).borderRadius),
    "6px",
    "login did not load npm-built FastUI controls",
  );
  await screenshot("login-desktop-light");
  const noScriptContext = await browser.newContext({
    javaScriptEnabled: false,
    viewport: { width: 1440, height: 1000 },
  });
  try {
    const nativePage = await noScriptContext.newPage();
    await nativePage.goto(`${origin}/login`);
    await nativePage.locator("input[name=email]").fill("invited@example.test");
    await nativePage
      .locator("input[name=password]")
      .fill("invited-reader-password-123");
    await nativePage.getByRole("button", { name: "登录", exact: true }).click();
    await nativePage.waitForURL(`${origin}/console/`);
    const nativeIdentity = await (
      await noScriptContext.request.get(`${origin}/api/v1/me`)
    ).json();
    assert.equal(
      nativeIdentity.user.email,
      "invited@example.test",
      "native login required JavaScript",
    );
  } finally {
    await noScriptContext.close();
  }
  await page.locator("input[name=email]").fill("invited@example.test");
  await page
    .locator("input[name=password]")
    .fill("invited-reader-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("heading", { name: "你好，Invited Reader" }).waitFor();
  assert.equal(await page.getByRole("button", { name: "应用管理", exact: true }).count(), 0);
  assert.equal(await page.getByRole("button", { name: "调用策略", exact: true }).count(), 0);
  assert.equal(await page.getByRole("button", { name: "失败投递", exact: true }).count(), 0);
  await preserveSecurityDraft();
  await page.getByLabel("原 FastCAS 密码").fill("invited-reader-password-123");
  await page
    .getByLabel("新 FastCAS 密码", { exact: true })
    .fill("invited-reader-changed-456");
  await page
    .getByLabel("确认 FastCAS 新密码")
    .fill("invited-reader-changed-456");
  await page.getByRole("button", { name: "修改 FastCAS 密码" }).click();
  await page.getByRole("status").getByText("FastCAS 密码已更新").waitFor();
  await page.goto(`${origin}/login`);
  await page.locator("input[name=email]").fill("invited@example.test");
  await page
    .locator("input[name=password]")
    .fill("invited-reader-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  assert.match(
    await page.locator("body").innerText(),
    /authentication_required/,
    "old self-service password was accepted",
  );
  await page.goto(`${origin}/login`);
  await page.locator("input[name=email]").fill("invited@example.test");
  await page.locator("input[name=password]").fill("invited-reader-changed-456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("heading", { name: "你好，Invited Reader" }).waitFor();
  await page.getByRole("button", { name: "登录会话" }).click();
  await page.getByRole("heading", { name: "登录会话" }).waitFor();
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
    "desktop console overflow",
  );
  const device = await page.evaluate(() =>
    fetch("/oauth/device_authorization", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        client_id: "browser-device",
        scope: "openid",
      }),
    }).then((response) => response.json()),
  );
  assert.ok(device.user_code, "device code missing");
  await page.goto(
    `${origin}/device?user_code=${encodeURIComponent(device.user_code)}`,
    { waitUntil: "networkidle" },
  );
  await page.getByRole("heading", { name: "确认设备", exact: true }).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "允许这台设备" })
      .evaluate((element) => getComputedStyle(element).borderRadius),
    "6px",
    "device page did not load FastUI controls",
  );
  await screenshot("device-desktop-light");
  await page.getByRole("button", { name: "拒绝", exact: true }).click();
  await page.getByRole("heading", { name: "设备确认完成" }).waitFor();
  await page.getByRole("link", { name: "返回账号中心" }).click();
  await page.getByRole("heading", { name: "你好，Invited Reader" }).waitFor();
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await page.getByRole("heading", { name: "登录 FastCAS" }).waitFor();
  await page.goto(
    `${origin}/console/?flow=recovery&token=${encodeURIComponent(recovery)}`,
    { waitUntil: "networkidle" },
  );
  await page.waitForURL((url) => !url.search);
  assert.equal(
    new URL(page.url()).search,
    "",
    "recovery token remained in URL",
  );
  await page
    .getByLabel("新密码", { exact: true })
    .fill("alice-replaced-password-123");
  await page.getByLabel("确认新密码").fill("alice-replaced-password-123");
  await page.getByRole("button", { name: "更新 FastCAS 密码" }).click();
  await page.getByRole("status").getByText("密码已更新").waitFor();
  await page.getByRole("link", { name: "前往登录" }).first().click();
  await page.locator("input[name=email]").fill("alice@example.test");
  await page
    .locator("input[name=password]")
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const rejection = await page.locator("body").innerText();
  if (!rejection.includes("authentication_required"))
    throw Error("old password was not rejected");
  await page.goto(`${origin}/login`);
  await page.locator("input[name=email]").fill("alice@example.test");
  await page
    .locator("input[name=password]")
    .fill("alice-replaced-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("heading", { name: "你好，Alice" }).waitFor();
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await page.getByRole("heading", { name: "登录 FastCAS" }).waitFor();
  await page.goto(`${origin}/login`);
  await page.locator("input[name=email]").fill("admin@example.test");
  await page
    .locator("input[name=password]")
    .fill("administrator long password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("heading", { name: "你好，Administrator" }).waitFor();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByLabel("界面主题").selectOption("light");
  assert.equal(
    await page.locator("html").getAttribute("data-fastui-theme"),
    "light",
  );
  await screenshot("security-desktop-light");
  await page.getByLabel("界面主题").selectOption("dark");
  assert.equal(
    await page.locator("html").getAttribute("data-fastui-theme"),
    "dark",
  );
  await screenshot("security-desktop-dark");
  await page.reload({ waitUntil: "networkidle" });
  assert.equal(
    await page.locator("html").getAttribute("data-fastui-theme"),
    "dark",
    "theme preference did not persist",
  );
  await page.getByLabel("界面主题").selectOption("light");

  const unverified = await page.evaluate(() =>
    fetch("/api/v1/admin/applications", { credentials: "same-origin" }).then(
      (r) => r.status,
    ),
  );
  assert.equal(unverified, 403, "administrator access succeeded before MFA");
  await page.getByRole("button", { name: "设置认证器" }).click();
  const uri = await page.getByLabel("认证器配置").inputValue();
  await page.getByLabel("动态验证码或恢复码").fill(totp(uri));
  await page.getByRole("button", { name: "确认验证码并开启" }).click();
  await page.getByText("双重验证：已启用").waitFor();
  const originalRecovery = await page.locator("pre").innerText();
  await page.getByRole("button", { name: "更新恢复码" }).click();
  await acceptConfirmation();
  await page.getByRole("status").getByText("恢复码已更新").waitFor();
  assert.notEqual(
    await page.locator("pre").innerText(),
    originalRecovery,
    "recovery codes did not rotate",
  );
  await page.getByRole("button", { name: "身份管理" }).click();
  await page.getByRole("heading", { name: "身份管理" }).waitFor();
  assert.equal(
    await page.getByText("zz-user-105@example.test").count(),
    0,
    "old identity was visible before pagination",
  );
  await page.getByRole("button", { name: "加载更多身份" }).click();
  await page.getByText("zz-user-105@example.test").waitFor();
  await page.getByRole("button", { name: "审计", exact: true }).click();
  await page.getByRole("heading", { name: "最近审计" }).waitFor();
  assert.equal(
    await page.getByText("audit-page-001").count(),
    0,
    "old audit was visible before pagination",
  );
  await auditAction("加载更多审计");
  await page.getByText("audit-page-001").waitFor();
  await page.getByLabel("操作名称").fill("audit.page");
  await page.getByLabel("执行者 ID").fill("browser-contract");
  await auditAction("筛选记录");
  await page.getByText("audit-page-105").waitFor();
  assert.equal(
    await page.getByText("audit-page-001").count(),
    0,
    "audit filtering did not reset pagination",
  );
  await auditAction("加载更多审计");
  await page.getByText("audit-page-001").waitFor();
  await page.getByLabel("目标 ID").fill("audit-page-005");
  await auditAction("筛选记录");
  await page.getByText("audit-page-005").waitFor();
  assert.equal(
    await page.locator("tbody tr").count(),
    1,
    "combined audit filters returned unrelated events",
  );
  await screenshot("audit-desktop");
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
    "audit table overflowed the viewport",
  );
  await page.getByLabel("目标 ID").fill("no-such-target");
  await auditAction("筛选记录");
  await page.getByRole("heading", { name: "没有匹配的审计记录" }).waitFor();
  await auditAction("重置筛选");
  await page.getByRole("button", { name: "加载更多审计" }).waitFor();

  const invalidCursor = await page.evaluate(() =>
    fetch("/api/v1/admin/audit-events?before=invalid", {
      credentials: "same-origin",
    }).then((r) => r.status),
  );
  assert.equal(invalidCursor, 403, "invalid audit cursor was accepted");
  await page.getByRole("button", { name: "失败投递" }).click();
  await page.getByRole("heading", { name: "失败投递" }).waitFor();
  await page.getByText("browser-contract-dead").waitFor();
  await page.getByRole("button", { name: "查看投递记录" }).click();
  await page.getByText(/第 12 次 · http_error · HTTP 503 · 42 毫秒/).waitFor();
  assert.equal(
    (await page.locator("body").innerText()).includes("never-return-this"),
    false,
    "delivery payload leaked to console",
  );
  await page.getByRole("button", { name: "应用管理" }).click();
  await page.getByRole("heading", { name: "登记应用", exact: true }).waitFor();
  await page.getByLabel("应用 ID").fill("browser-contract-app");
  await page.getByLabel("名称", { exact: true }).fill("Browser Contract");
  await page
    .getByLabel("回调地址（每行一个）")
    .fill("https://app.example.test/callback");
  await page.getByRole("button", { name: "登记应用", exact: true }).click();
  await page.getByLabel("客户端密钥").waitFor();
  const issuedSecret = await page.getByLabel("客户端密钥").inputValue();
  const appRow = page
    .locator("article")
    .filter({ hasText: "browser-contract-app" });
  await appRow.getByRole("button", { name: "配置权限" }).click();
  const permissionDialog = page.getByRole("dialog", { name: "配置应用权限" });
  await permissionDialog
    .getByLabel("允许的 Scope")
    .fill("openid profile email");
  await permissionDialog.getByRole("button", { name: "保存配置" }).click();
  await page.getByText("应用配置已更新").waitFor();
  await appRow.getByRole("button", { name: "配置通知地址" }).click();
  let callbackDialog = page.getByRole("dialog", { name: "配置通知地址" });
  await callbackDialog
    .getByLabel("绑定事件接收地址")
    .fill("http://insecure.example.test/events");
  await callbackDialog.getByRole("button", { name: "保存配置" }).click();
  await callbackDialog.getByRole("alert").waitFor();
  assert.equal(
    await callbackDialog.isVisible(),
    true,
    "failed save closed the editor",
  );
  await callbackDialog
    .getByLabel("绑定事件接收地址")
    .fill("https://app.example.test/events");
  await callbackDialog
    .getByLabel("退出通知接收地址")
    .fill("https://app.example.test/logout");
  await callbackDialog.getByRole("button", { name: "保存配置" }).click();
  await page.getByText("应用配置已更新").waitFor();
  await appRow.getByRole("button", { name: "配置通知地址" }).click();
  callbackDialog = page.getByRole("dialog", { name: "配置通知地址" });
  assert.equal(
    await callbackDialog.getByLabel("绑定事件接收地址").inputValue(),
    "https://app.example.test/events",
    "saved callback was not retained",
  );
  await page.keyboard.press("Escape");
  await callbackDialog.waitFor({ state: "hidden" });
  assert.equal(
    await appRow
      .getByRole("button", { name: "配置通知地址" })
      .evaluate((element) => element === document.activeElement),
    true,
    "dialog did not restore focus",
  );

  const registered = await page.evaluate(() =>
    fetch("/api/v1/admin/applications", { credentials: "same-origin" }).then(
      (r) => r.json(),
    ),
  );
  assert.ok(
    registered.some((item) => item.id === "browser-contract-app"),
    "application was not stored",
  );
  assert.deepEqual(
    registered.find((item) => item.id === "browser-contract-app").scopes,
    ["openid", "profile", "email"],
  );
  assert.equal(
    registered.find((item) => item.id === "browser-contract-app").events_uri,
    "https://app.example.test/events",
  );
  assert.equal(
    await page.getByText("zz-app-105").count(),
    0,
    "old app was visible before pagination",
  );
  await page.getByRole("button", { name: "加载更多应用" }).click();
  await page.getByText("zz-app-105").waitFor();
  assert.deepEqual(
    await page.evaluate(() =>
      Object.keys(localStorage).filter((key) => key !== "fastcas.color-mode"),
    ),
    [],
    "one-time secret stored in localStorage",
  );
  await page
    .locator("article")
    .filter({ hasText: "browser-contract-app" })
    .getByRole("button", { name: "轮换密钥" })
    .click();
  await page
    .getByRole("dialog", { name: "确认操作" })
    .getByRole("button", { name: "取消" })
    .click();
  assert.equal(
    await page.getByLabel("客户端密钥").inputValue(),
    issuedSecret,
    "cancelled rotation changed secret",
  );
  await page
    .locator("article")
    .filter({ hasText: "browser-contract-app" })
    .getByRole("button", { name: "轮换密钥" })
    .click();
  await acceptConfirmation();
  let rotatedSecret = issuedSecret;
  for (
    let attempt = 0;
    attempt < 100 && rotatedSecret === issuedSecret;
    attempt++
  ) {
    await page.waitForTimeout(50);
    rotatedSecret = await page.getByLabel("客户端密钥").inputValue();
  }
  assert.notEqual(rotatedSecret, issuedSecret, "rotated secret unchanged");
  await page.getByRole("button", { name: "开发者接入" }).click();
  await page.getByRole("heading", { name: "接入顺序" }).waitFor();
  await page.locator("code").filter({ hasText: origin }).first().waitFor();
  await page.getByText("browser-contract-app").waitFor();
  assert.equal(
    (await page.locator("body").innerText()).includes(rotatedSecret),
    false,
    "client secret leaked to integration guide",
  );
  await page.getByRole("button", { name: "应用管理" }).click();
  await page.getByRole("heading", { name: "登记应用", exact: true }).waitFor();
  assert.equal(
    await page.getByLabel("客户端密钥", { exact: true }).count(),
    0,
    "one-time secret survived leaving applications",
  );
  await page.getByRole("button", { name: "开发者接入" }).click();
  await page.getByRole("heading", { name: "接入顺序" }).waitFor();

  await page.getByRole("button", { name: "加载更多应用配置" }).click();
  await page.getByText("zz-app-105").waitFor();
  await page.getByRole("button", { name: "服务身份" }).click();
  await page.getByRole("heading", { name: "登记服务身份" }).waitFor();
  await page.getByLabel("服务 ID").fill("browser-contract-service");
  await page
    .getByLabel("名称", { exact: true })
    .fill("Browser Contract Service");
  await page.getByLabel("允许的 Scope").fill("insight:publish");
  await page.getByLabel("资源受众").fill("research-api");
  await page.getByRole("button", { name: "登记服务" }).click();
  await page.getByLabel("服务客户端密钥").waitFor();
  const serviceSecret = await page.getByLabel("服务客户端密钥").inputValue();
  assert.ok(serviceSecret.length > 20, "service secret missing");
  const services = await page.evaluate(() =>
    fetch("/api/v1/admin/service-accounts", {
      credentials: "same-origin",
    }).then((r) => r.json()),
  );
  assert.ok(
    services.some(
      (item) => item.client.id === "browser-contract-service" && item.active,
    ),
    "service account was not stored",
  );
  assert.equal(
    await page.getByText("zz-service-105").count(),
    0,
    "old service was visible before pagination",
  );
  await page.getByRole("button", { name: "加载更多服务身份" }).click();
  await page.getByText("zz-service-105").waitFor();
  assert.deepEqual(
    await page.evaluate(() =>
      Object.keys(localStorage).filter((key) => key !== "fastcas.color-mode"),
    ),
    [],
    "one-time service secret stored in localStorage",
  );
  const createdService = page
    .locator("article")
    .filter({ hasText: "browser-contract-service" });
  await createdService
    .getByRole("button", { name: "停用", exact: true })
    .click();
  await acceptConfirmation();
  await createdService.getByRole("button", { name: "重新启用" }).waitFor();
  await createdService.getByRole("button", { name: "重新启用" }).click();
  await page.getByLabel("服务客户端密钥").evaluate((element, original) => {
    if (element.value === original)
      throw Error("re-enabled service secret unchanged");
  }, serviceSecret);
  await page.getByRole("button", { name: "邀请与恢复" }).click();
  await page.getByLabel("目标邮箱").fill("another@example.test");
  await page.getByRole("button", { name: "生成一次性凭证" }).click();
  await page.getByLabel("一次性凭证").waitFor();
  const priorCredential = await page.getByLabel("一次性凭证").inputValue();
  await page.getByLabel("操作").selectOption("mfa_reset");
  await page.getByLabel("目标邮箱").fill("admin@example.test");
  await page.getByRole("button", { name: "生成一次性凭证" }).click();
  await page.waitForFunction((previous) => {
    const field = document.querySelector('textarea[aria-label="一次性凭证"]');
    return (
      field instanceof HTMLTextAreaElement &&
      field.value.length > 20 &&
      field.value !== previous
    );
  }, priorCredential);
  const resetCredential = await page.getByLabel("一次性凭证").inputValue();
  assert.ok(resetCredential.length > 20, "MFA reset credential missing");
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await page.getByRole("heading", { name: "登录 FastCAS" }).waitFor();
  await page.goto(
    `${origin}/console/?flow=mfa_reset&token=${encodeURIComponent(resetCredential)}`,
    { waitUntil: "networkidle" },
  );
  await page.waitForURL((url) => !url.search);
  await page
    .getByLabel("当前 FastCAS 密码")
    .fill("administrator long password");
  const resetResponsePromise = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/v1/recover-mfa",
  );
  await page.getByRole("button", { name: "重置双重验证" }).click();
  const resetResponse = await resetResponsePromise;
  assert.equal(resetResponse.status(), 200, "MFA reset request failed");
  await page.getByRole("status").getByText("双重验证已重置").waitFor();
  await page.getByRole("link", { name: "前往登录" }).first().click();
  await page.locator("input[name=email]").fill("admin@example.test");
  await page
    .locator("input[name=password]")
    .fill("administrator long password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByText("双重验证：未启用").waitFor();
  const resetAdminAccess = await page.evaluate(() =>
    fetch("/api/v1/admin/applications", { credentials: "same-origin" }).then(
      (r) => r.status,
    ),
  );
  assert.equal(
    resetAdminAccess,
    403,
    "administrator retained management access after MFA reset",
  );
  assert.deepEqual(uiErrors, [], "browser JavaScript errors");
  await writeFile("../.local/ui-verification/integration-results.json", JSON.stringify({ checks: integrationChecks, browserErrors: uiErrors, consoleContract: "passed" }, null, 2));
  console.log(
    "FastCAS browser contract passed: invite, login, logout, password and MFA recovery, MFA enrollment and recovery-code rotation, audit filtering and pagination, FastUI light/dark desktop themes, accessible application configuration dialogs, cancelled rotation, one-time secret isolation, delivery history, developer integration guide, application registration, service lifecycle, secret rotation and invitation",
  );
} catch (error) {
  console.error(
    "browser state:",
    page.url(),
    (
      await page
        .locator("body")
        .innerText()
        .catch(() => "")
    ).slice(0, 700),
  );
  throw error;
} finally {
  await browser.close();
}
