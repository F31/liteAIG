import { test, expect } from "@playwright/test";

// The account menu / reset-code strings depend on the SMTP sink the
// playwright config boots on 127.0.0.1:18026.
const SMTP_HTTP = "http://127.0.0.1:18026";

// Browser E2E of the full management loop against the real Lite binary
// (embedded Console + Admin API on the same origin, temp SQLite DB).
test("setup → login → dashboard → playground → requests → config", async ({
  page,
}) => {
  // 1. Setup wizard closure.
  await page.goto("/setup");
  // The console defaults to Chinese; switch to English for stable labels.
  await page.getByRole("button", { name: "语言" }).click();
  await page.getByLabel("Administrator username").fill("admin");
  await page.getByLabel("Administrator password").fill("password-123456");
  await page.getByLabel("Tenant name").fill("Acme");
  // Pick the OpenAI-compatible preset; it auto-expands the advanced fields
  // so the endpoint can be pointed at the local test provider.
  await page.locator(".ant-select").first().click();
  await page
    .locator(".ant-select-item-option", { hasText: "OpenAI Compatible" })
    .click();
  await page.getByLabel("Provider endpoint").fill("http://127.0.0.1:8092");
  await page.getByLabel("Provider credential").fill("sk-test-provider");
  await page
    .getByRole("button", { name: "Create initial configuration" })
    .click();

  // The wizard returns a one-time virtual key.
  await expect(page.getByText(/sk-lia-v1/).first()).toBeVisible({
    timeout: 20000,
  });
  // 2. Local login establishes a session and redirects to the dashboard.
  await page.goto("/login");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("password-123456");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "AI control plane" }),
  ).toBeVisible();

  // 3. Playground runs a governed request through the production pipeline
  // against the real (local) provider.
  await page.goto("/playground");
  await page.getByLabel("Input").fill("hello e2e");
  await page.getByRole("button", { name: "Run governed request" }).click();
  await expect(page.getByText("provider-echo")).toBeVisible({
    timeout: 20000,
  });

  // 3b. Streaming toggle: live SSE tokens from the provider, then the final
  // assembled output and usage summary.
  await page.getByLabel("Stream response").check();
  await page.getByLabel("Input").fill("hello stream");
  await page.getByRole("button", { name: "Run governed request" }).click();
  await expect(page.getByText("stream-ok")).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByText("Token usage: 8").first()).toBeVisible({
    timeout: 15000,
  });

  // 4. Request Explorer lists the recorded requests.
  await page.goto("/requests");
  await expect(page.getByText("Request Explorer")).toBeVisible();
  await expect(page.locator("table tbody tr").first()).toBeVisible();
  await expect(page.getByText("success").first()).toBeVisible();

  // 4b. Runtime Resources renders the seeded tenant resources.
  await page.goto("/resources");
  await expect(
    page.getByRole("heading", { name: "Runtime resources" }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Model access" }).click();
  await expect(page.locator(".ant-table-tbody tr").first()).toBeVisible();
  await page.getByRole("tab", { name: "Access keys" }).click();
  // The wizard's first key is a stable, deterministic row to wait for.
  await expect(page.getByText("first-key").first()).toBeVisible({
    timeout: 15000,
  });

  // 5. Config shows the published draft and its version.
  await page.goto("/config");
  await expect(page.getByText("Configuration lifecycle")).toBeVisible();
  await page.getByRole("combobox", { name: "Draft" }).click();
  await page.locator(".ant-select-item-option").first().click();
  await expect(page.getByText("Versions")).toBeVisible();
  await expect(page.locator(".ant-table-tbody tr").first()).toBeVisible();

  // 6. Email-verified password reset: the admin attaches a recovery
  // address, signs out, requests a code from the login screen, and finishes
  // the reset with the code the SMTP sink captured.
  await fetch(`${SMTP_HTTP}/messages`, { method: "DELETE" });

  // Attach a recovery email through the account menu (self-service). The
  // API response (not the transient toast) is the success signal.
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Set reset email" }).click();
  const emailDialog = page.getByRole("dialog");
  await emailDialog.getByLabel("Email").fill("admin@example.com");
  const [emailResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api/admin/users/me/email"),
    ),
    emailDialog.getByRole("button", { name: "Submit" }).click(),
  ]);
  expect(emailResponse.status()).toBe(200);
  // Changing the recovery address is treated as a credential change: the
  // backend voids the account's sessions, so the console returns to the
  // login screen automatically. Continue the reset from there.
  await expect(
    page.getByRole("button", { name: "Forgot password" }),
  ).toBeVisible({ timeout: 15000 });

  await page.getByRole("button", { name: "Forgot password" }).click();
  const resetDialog = page.getByRole("dialog");
  await resetDialog.getByLabel("Username").fill("admin");
  const [forgotResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api/admin/password/forgot"),
    ),
    resetDialog.getByRole("button", { name: "Send reset code" }).click(),
  ]);
  expect(forgotResponse.status()).toBe(202);
  await expect(resetDialog.getByLabel("Reset code")).toBeVisible();

  // Pull the one-time code back out of the SMTP sink.
  let code: string | undefined;
  await expect
    .poll(
      async () => {
        const response = await fetch(`${SMTP_HTTP}/messages`);
        const list = (await response.json()) as { body: string }[];
        code = list.at(-1)?.body.match(/\b\d{6}\b/)?.[0];
        return typeof code === "string";
      },
      { timeout: 15000 },
    )
    .toBe(true);
  if (!code) throw new Error("no 6-digit code captured by the SMTP sink");

  await resetDialog.getByLabel("Reset code").fill(code);
  await resetDialog.getByLabel("New password").fill("password-654321");
  const [confirmResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api/admin/password/reset/confirm"),
    ),
    resetDialog.getByRole("button", { name: "Confirm reset" }).click(),
  ]);
  expect(confirmResponse.status()).toBe(200);
  await expect(resetDialog).toBeHidden();

  // The new password signs in; the old one is gone.
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("password-654321");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "AI control plane" }),
  ).toBeVisible();
});
