import { test, expect } from "@playwright/test";
import type { Page, APIRequestContext } from "@playwright/test";

const CARD_URL = "http://127.0.0.1:8094";
const KEY_URL = `${CARD_URL}/key.pem`;

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

test("agent card discover → verify → review → activate", async ({
  page,
  request,
}: {
  page: Page;
  request: APIRequestContext;
}) => {
  await setupAndLogin(page);
  await page.goto("/governance");
  await page.getByRole("tab", { name: "Federated agents" }).click();

  // The card server exposes the RS256 public key; pasting it lets the Admin
  // verify the card signature during discovery.
  const keyPem = await (await request.get(KEY_URL)).text();
  await page.getByLabel("Agent Card URL").fill(CARD_URL);
  await page.getByLabel("Agent name (optional)").fill("peer-agent");
  await page.getByLabel("Verification key PEM (optional, RS256)").fill(keyPem);
  await page.getByRole("button", { name: "Discover and verify" }).click();

  // Discovery creates a candidate relationship whose anchor is verified.
  const candidateRow = page.getByRole("row", { name: /peer-agent/ });
  await expect(candidateRow).toBeVisible({ timeout: 20000 });
  await expect(candidateRow).toContainText("candidate");
  await expect(candidateRow).toContainText("✓");

  // Operator review activates the relationship (verified anchor + grants).
  await candidateRow.getByRole("button", { name: "Activate" }).click();
  // Assert durable state, not the transient success toast: the row flips to
  // active with the verified anchor and the action becomes Suspend.
  await expect(candidateRow).toContainText("active", { timeout: 20000 });
  await expect(candidateRow).toContainText("✓");
  await expect(
    candidateRow.getByRole("button", { name: "Suspend" }),
  ).toBeVisible();
});
