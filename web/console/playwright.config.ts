import { defineConfig } from "@playwright/test";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Boots the built Lite binary (embedded Console + Admin API) on a temp
// SQLite DB; each Playwright run gets a pristine instance. Two backends are
// booted so spec files that each perform the one-time setup wizard (they
// share no state, and workers run files in parallel) never race over one DB.
const binary = join(process.cwd(), "e2e", ".bin", "liteaig-e2e");
const dbFile = join(tmpdir(), "liteaig-e2e.db");
const dbFileCard = join(tmpdir(), "liteaig-e2e-card.db");
const dbFileAlerts = join(tmpdir(), "liteaig-e2e-alerts.db");
const adminAddr = "127.0.0.1:8090";
const readyAddr = "127.0.0.1:8091";
const gatewayAddr = "127.0.0.1:8093";
const cardAdminAddr = "127.0.0.1:8095";
const cardReadyAddr = "127.0.0.1:8096";
const cardGatewayAddr = "127.0.0.1:8097";
const alertsAdminAddr = "127.0.0.1:8100";
const alertsReadyAddr = "127.0.0.1:8101";
const alertsGatewayAddr = "127.0.0.1:8102";

const liteCmd = (db: string, admin: string, ready: string, gateway: string) =>
  `rm -f "${db}" && exec "${binary}" --db file:${db} --admin-addr ${admin} --ready-addr ${ready} --gateway-addr ${gateway} --smtp-host 127.0.0.1 --smtp-port 18025 --smtp-tls-mode none --smtp-from noreply@liteaig.local`;

export default defineConfig({
  testDir: "./e2e",
  timeout: 120000,
  expect: { timeout: 15000 },
  webServer: [
    {
      command: "node e2e/provider-server.mjs",
      url: "http://127.0.0.1:8092/healthz",
      reuseExistingServer: false,
      timeout: 30000,
    },
    {
      // SMTP sink that captures the password-reset emails; the spec reads
      // the one-time code back over the /messages endpoint.
      command: "node e2e/smtp-sink.mjs",
      url: "http://127.0.0.1:18026/healthz",
      reuseExistingServer: false,
      timeout: 30000,
    },
    {
      // Signed A2A Agent Card producer used by the federation review UX spec.
      command: "node e2e/card-server.mjs",
      url: "http://127.0.0.1:8094/healthz",
      reuseExistingServer: false,
      timeout: 30000,
    },
    {
      command: liteCmd(dbFile, adminAddr, readyAddr, gatewayAddr),
      // Probe the admin API (not /readyz): readyz is gated on a published
      // tenant runtime, which the test creates via the setup wizard.
      url: "http://127.0.0.1:8090/api/admin/oidc/config",
      reuseExistingServer: false,
      timeout: 60000,
    },
    {
      command: liteCmd(
        dbFileCard,
        cardAdminAddr,
        cardReadyAddr,
        cardGatewayAddr,
      ),
      url: "http://127.0.0.1:8095/api/admin/oidc/config",
      reuseExistingServer: false,
      timeout: 60000,
    },
    {
      command: liteCmd(
        dbFileAlerts,
        alertsAdminAddr,
        alertsReadyAddr,
        alertsGatewayAddr,
      ),
      url: "http://127.0.0.1:8100/api/admin/oidc/config",
      reuseExistingServer: false,
      timeout: 60000,
    },
  ],
  use: {
    headless: true,
  },
  projects: [
    {
      name: "management-loop",
      testMatch: /management-loop\.spec\.ts/,
      use: { baseURL: "http://127.0.0.1:8090" },
    },
    {
      name: "agent-card-review",
      testMatch: /agent-card-review\.spec\.ts/,
      use: { baseURL: "http://127.0.0.1:8095" },
    },
    {
      name: "alerts-notifications",
      testMatch: /alerts-notifications\.spec\.ts/,
      use: { baseURL: "http://127.0.0.1:8100" },
    },
  ],
});
