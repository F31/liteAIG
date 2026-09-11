import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfigProvider } from "antd";
import axe from "axe-core";
import "./i18n";
import { App } from "./App";

function renderApp(path = "/dashboard") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <ConfigProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[path]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          csrfToken: "csrf",
          scopes: [],
          ready: false,
          drift: "none",
          circuits: [],
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    ),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test("health and alerts navigation is localized", async () => {
  renderApp("/health");
  expect(await screen.findByText("健康与熔断")).toBeInTheDocument();
  expect(screen.getAllByText("告警").length).toBeGreaterThan(0);
});

test("health page has no serious accessibility violations", async () => {
  const { container } = renderApp("/health");
  await screen.findByText("健康与熔断");
  const result = await axe.run(container, {
    runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
    rules: { "color-contrast": { enabled: false } },
  });
  const serious = result.violations.filter((item) =>
    ["serious", "critical"].includes(item.impact ?? ""),
  );
  for (const violation of result.violations) {
    console.log(
      "AXE",
      violation.id,
      violation.nodes.map((node) => node.target.join(" ")).join(" | "),
    );
  }
  expect(serious.map((v) => v.id)).toEqual([]);
});
