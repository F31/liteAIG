import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";

const WEBHOOK = "http://127.0.0.1:8092/hook";

// Walks the setup wizard (pointed at the local provider-server.mjs) then logs
// in as the first administrator, matching the other Console specs.
async function setupAndLogin(page: Page) {
  await page.goto("/setup");
  await page.getByRole("button", { name: "语言" }).click();
  await page.getByLabel("Administrator username").fill("admin");
  await page.getByLabel("Administrator password").fill("password-123456");
  await page.getByLabel("Tenant name").fill("Acme");
  await page.locator(".ant-select").first().click();
  await page
    .locator(".ant-select-item-option", { hasText: "OpenAI Compatible" })
    .click();
  await page.getByLabel("Provider endpoint").fill("http://127.0.0.1:8092");
  await page.getByLabel("Provider credential").fill("sk-test-provider");
  await page
    .getByRole("button", { name: "Create initial configuration" })
    .click();
  await expect(page.getByText(/sk-lia-v1/).first()).toBeVisible({
    timeout: 20000,
  });
  await page.goto("/login");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("password-123456");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "AI control plane" }),
  ).toBeVisible();
}

test("notification target saves, persists and disables through the Alerts page", async ({
  page,
}: {
  page: Page;
}) => {
  await setupAndLogin(page);
  await page.goto("/alerts");
  await expect(
    page.getByRole("heading", { name: "Alert Inbox" }),
  ).toBeVisible();

  const target = page.getByLabel("Webhook URL");
  await target.fill(WEBHOOK);

  // Saving must round-trip through the Admin API (200) and hot-reload state.
  const putResponse = page.waitForResponse(
    (r) =>
      r.url().includes("/api/admin/alerts/notifications") &&
      r.request().method() === "PUT",
  );
  await page.getByRole("button", { name: "Submit" }).click();
  expect((await putResponse).status()).toBe(200);

  // The value survives the GET refetch (server-persisted desired state).
  await expect(target).toHaveValue(WEBHOOK, { timeout: 10000 });

  // Disabling sends DELETE and clears the persisted target.
  const deleteResponse = page.waitForResponse(
    (r) =>
      r.url().includes("/api/admin/alerts/notifications") &&
      r.request().method() === "DELETE",
  );
  await page.getByRole("button", { name: "Disable notifications" }).click();
  expect((await deleteResponse).status()).toBe(200);
  await expect(target).toHaveValue("", { timeout: 10000 });
});
