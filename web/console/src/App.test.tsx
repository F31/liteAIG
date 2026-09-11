import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfigProvider } from "antd";
import axe from "axe-core";
import "./i18n";
import { App } from "./App";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ csrfToken: "csrf", scopes: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

function renderApp() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <ConfigProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
test("renders localized navigation", async () => {
  renderApp();
  expect(await screen.findByText("AI 控制平面")).toBeInTheDocument();
  expect(
    screen.getAllByRole("navigation", { name: "主导航" }).length,
  ).toBeGreaterThan(0);
});
test("dashboard has no serious accessibility violations", async () => {
  const { container } = renderApp();
  await screen.findByText("AI 控制平面");
  const result = await axe.run(container, {
    runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(
    result.violations.filter((item) =>
      ["serious", "critical"].includes(item.impact ?? ""),
    ),
  ).toHaveLength(0);
});
