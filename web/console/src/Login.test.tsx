import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfigProvider } from "antd";
import "./i18n";
import Login from "./pages/Login";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function mockFetch(
  routes: Array<{
    test: (url: string, init: RequestInit | undefined) => boolean;
    status?: number;
    body?: unknown;
  }>,
) {
  const calls: Array<{ url: string; body?: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const route = routes.find((item) => item.test(url, init));
      if (!route)
        throw new Error(`unmocked fetch ${init?.method ?? "GET"} ${url}`);
      calls.push({ url, body: init?.body as string | undefined });
      return new Response(JSON.stringify(route.body ?? {}), {
        status: route.status ?? 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

function stubMatchMedia() {
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

function renderLogin() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <ConfigProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/login"]}>
          <Login />
        </MemoryRouter>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

test("offers localized local and OIDC sign-in", async () => {
  stubMatchMedia();
  mockFetch([
    { test: (url) => url.endsWith("/oidc/config"), body: { enabled: true } },
    {
      test: (url) => url.endsWith("/password/forgot/config"),
      body: { enabled: false },
    },
  ]);
  renderLogin();
  expect(
    await screen.findByRole("button", { name: /登\s*录/ }),
  ).toBeInTheDocument();
  expect(
    await screen.findByRole("button", { name: "忘记密码" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "忘记密码" }));
  expect((await screen.findAllByText("重置密码")).length).toBeGreaterThan(0);
  expect(
    await screen.findByRole("button", { name: "使用单点登录" }),
  ).toBeInTheDocument();
});

test("email reset flow sends the code then confirms with code and new password", async () => {
  stubMatchMedia();
  const calls = mockFetch([
    { test: (url) => url.endsWith("/oidc/config"), body: { enabled: false } },
    {
      test: (url) => url.endsWith("/password/forgot/config"),
      body: { enabled: true },
    },
    {
      test: (url, init) =>
        init?.method === "POST" && url.endsWith("/password/forgot"),
      status: 202,
      body: { ok: true },
    },
    {
      test: (url, init) =>
        init?.method === "POST" && url.endsWith("/password/reset/confirm"),
      body: { ok: true },
    },
  ]);
  renderLogin();
  fireEvent.click(await screen.findByRole("button", { name: "忘记密码" }));

  const dialog = await screen.findByRole("dialog");
  expect(
    await within(dialog).findByRole("button", { name: "发送验证码" }),
  ).toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText("用户名"), {
    target: { value: "admin" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "发送验证码" }));

  await waitFor(() =>
    expect(
      calls.some(
        (call) =>
          call.url.endsWith("/password/forgot") &&
          call.body?.includes('"username":"admin"'),
      ),
    ).toBe(true),
  );

  // Step 2 asks for the code and a new password, then confirms.
  await waitFor(() =>
    expect(within(dialog).getByLabelText("验证码")).toBeInTheDocument(),
  );
  fireEvent.change(within(dialog).getByLabelText("验证码"), {
    target: { value: "123456" },
  });
  fireEvent.change(within(dialog).getByLabelText("新密码"), {
    target: { value: "new-password-1" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "确认重置" }));

  await waitFor(() =>
    expect(
      calls.some(
        (call) =>
          call.url.endsWith("/password/reset/confirm") &&
          call.body?.includes('"username":"admin"') &&
          call.body?.includes('"code":"123456"') &&
          call.body?.includes('"newPassword":"new-password-1"'),
      ),
    ).toBe(true),
  );
});

test("email reset flow falls back to the local emergency form when no mail is configured", async () => {
  stubMatchMedia();
  mockFetch([
    { test: (url) => url.endsWith("/oidc/config"), body: { enabled: false } },
    {
      test: (url) => url.endsWith("/password/forgot/config"),
      body: { enabled: false },
    },
  ]);
  renderLogin();
  fireEvent.click(await screen.findByRole("button", { name: "忘记密码" }));

  const dialog = await screen.findByRole("dialog");
  expect(await within(dialog).findByText(/本地紧急重置/)).toBeInTheDocument();
  expect(within(dialog).getByLabelText("新密码")).toBeInTheDocument();
  expect(
    within(dialog).queryByRole("button", { name: "发送验证码" }),
  ).not.toBeInTheDocument();
});
