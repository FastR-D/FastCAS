import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright-core";

const origin = process.env.FASTCAS_CONTRACT_ORIGIN;
const codes = JSON.parse(process.env.FASTCAS_STEP_UP_CODES);
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome",
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    {
      name: "fastcas_session",
      value: process.env.FASTCAS_STEP_UP_SESSION,
      url: origin,
      httpOnly: true,
    },
    {
      name: "fastcas_csrf",
      value: process.env.FASTCAS_STEP_UP_CSRF,
      url: origin,
    },
  ]);
  const page = await context.newPage();
  const errors = [];
  const proofs = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/me/mfa/verify")
      proofs.push(request.postDataJSON());
  });
  const modal = page.getByRole("dialog", { name: "安全验证", exact: true });
  const verify = async (code) => {
    await modal.getByLabel("授权码", { exact: true }).fill(code);
    await modal
      .getByRole("button", { name: "验证并继续", exact: true })
      .click();
  };
  const expire = async () =>
    assert.equal(
      (await context.request.post(origin + "/__test/expire-proof")).status(),
      204,
    );
  await page.goto(origin + "/console/", { waitUntil: "networkidle" });
  assert.equal(
    await page.getByRole("heading", { name: "身份验证", exact: true }).count(),
    0,
  );
  await page.getByRole("button", { name: "用户管理", exact: true }).click();
  await modal.waitFor();
  assert.equal(await modal.locator('input[type="password"]').count(), 0);
  assert.equal(
    (await page.locator("body").innerText()).includes(
      "Administrator access requires",
    ),
    false,
  );
  await modal.getByRole("button", { name: "取消", exact: true }).click();
  await modal.waitFor({ state: "hidden" });
  assert.equal(
    await page.getByRole("alert").count(),
    0,
    "cancel produced an error banner",
  );
  await page.getByRole("button", { name: "安全设置", exact: true }).click();
  await page.getByRole("button", { name: "用户管理", exact: true }).click();
  await modal.waitFor();
  await verify("invalid-code");
  await modal
    .getByRole("alert")
    .getByText("授权码无效或已使用，请重试")
    .waitFor();
  assert.equal(await modal.isVisible(), true, "invalid proof closed the modal");
  await mkdir("../.local/ui-verification", { recursive: true });
  await page.screenshot({
    path: "../.local/ui-verification/step-up-desktop.png",
    fullPage: true,
  });
  await verify(codes[0]);
  await modal.waitFor({ state: "hidden" });
  await page.getByRole("table", { name: "用户列表" }).waitFor();

  await expire();
  await page.getByRole("button", { name: "邀请与恢复", exact: true }).click();
  await page.getByLabel("目标邮箱").fill("stepup-invite@example.test");
  await page
    .getByRole("button", { name: "生成一次性凭证", exact: true })
    .click();
  await modal.waitFor();
  await page.keyboard.press("Escape");
  await modal.waitFor({ state: "hidden" });
  assert.equal(
    await page.getByLabel("一次性凭证", { exact: true }).count(),
    0,
    "cancelled write was executed",
  );
  await page
    .getByRole("button", { name: "生成一次性凭证", exact: true })
    .click();
  await modal.waitFor();
  await verify(codes[1]);
  await modal.waitFor({ state: "hidden" });
  await page.getByLabel("一次性凭证", { exact: true }).waitFor();

  await expire();
  await page.setViewportSize({ width: 393, height: 851 });
  await page
    .getByRole("button", { name: "生成一次性凭证", exact: true })
    .click();
  await modal.waitFor();
  await page.screenshot({
    path: "../.local/ui-verification/step-up-mobile.png",
    fullPage: false,
  });
  const bounds = await modal.boundingBox();
  assert.ok(
    bounds.x >= 0 && bounds.x + bounds.width <= 394,
    "mobile modal overflows viewport",
  );
  await modal.getByRole("button", { name: "取消", exact: true }).click();
  await modal.waitFor({ state: "hidden" });
  assert.equal(proofs.length, 3);
  assert.ok(
    proofs.every(
      (proof) =>
        Object.keys(proof).length === 1 && typeof proof.code === "string",
    ),
    "step-up requested a password",
  );
  assert.deepEqual(errors, []);
  console.log(
    "Step-up browser contract passed: code-only prompt, invalid proof, cancel and Escape, automatic read/write retry, no duplicate write, desktop and mobile",
  );
} finally {
  await browser.close();
}
